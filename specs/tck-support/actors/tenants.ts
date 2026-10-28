import { strict as assert } from 'node:assert';
import { expectData, expectError, login, mutate, newContext, runSuffix } from './index';
import type { Actor, Playwright, Role } from './index';

/** The synthetic password every seeded account signs in with. */
export const SEEDED_PASSWORD = 'tck-password-0001';

export interface SeededTenant {
  id: string;
  slug: string;
  admin: Actor;
  /** Keyed by the label passed in SeedOptions.members. */
  members: Record<string, Actor>;
}

export interface SeedOptions {
  /** Slug base; the helper appends -<runSuffix>. */
  slugBase: string;
  members?: Array<{ label: string; role: Role }>;
}

interface TenantCreatedView {
  tenant: { id: string; slug: string };
  invitation: { id: string };
  invitationUrl: string;
}

/** Reads the token from the fragment of an invitation URL (`...#token=<token>`). */
export function tokenFromInvitationUrl(invitationUrl: string): string {
  const fragment = new URL(invitationUrl).hash;
  const prefix = '#token=';
  assert.ok(fragment.startsWith(prefix), 'the invitation URL carries no #token= fragment');
  return fragment.slice(prefix.length);
}

async function switchTo(actor: Actor, tenantId: string): Promise<void> {
  const response = await mutate(actor, 'POST', '/api/v1/auth/switch-tenant', { tenantId });
  await expectData<unknown>(response, 200);
}

/** POST /api/v1/invitations/accept-new, then login and switch-tenant onto the tenant. */
export async function joinAsNewAccount(playwright: Playwright, token: string, email: string, tenantId: string): Promise<Actor> {
  const anonymous = await newContext(playwright);
  const accepted = await anonymous.post('/api/v1/invitations/accept-new', {
    data: { token, password: SEEDED_PASSWORD, fullName: 'TCK Seeded User' },
  });
  await expectData<unknown>(accepted, 201);
  await anonymous.dispose();

  const actor = await login(playwright, email, SEEDED_PASSWORD);
  await switchTo(actor, tenantId);
  return actor;
}

/** Admin invites email with role; returns the token read from invitationUrl's fragment. */
export async function invite(admin: Actor, email: string, role: Role): Promise<{ invitationId: string; token: string }> {
  const response = await mutate(admin, 'POST', '/api/v1/invitations', { email, role });
  const created = await expectData<{ invitation: { id: string }; invitationUrl: string }>(response, 201);
  return { invitationId: created.invitation.id, token: tokenFromInvitationUrl(created.invitationUrl) };
}

/**
 * Operator POST /tenants with adminEmail, accept-new from the fragment, admin login,
 * admin switch-tenant onto it, then each member invited, accept-new, login, switch.
 * Emails are `<label>-<runSuffix>@example.com`; the admin's label is `<slugBase>-admin`.
 */
export async function seedTenant(playwright: Playwright, operator: Actor, options: SeedOptions): Promise<SeededTenant> {
  const suffix = runSuffix();
  const slug = `${options.slugBase}-${suffix}`;
  const adminEmail = `${options.slugBase}-admin-${suffix}@example.com`;

  const response = await mutate(operator, 'POST', '/api/v1/tenants', { slug, name: slug, adminEmail });
  const created = await expectData<TenantCreatedView>(response, 201);
  const tenantId = created.tenant.id;
  const admin = await joinAsNewAccount(playwright, tokenFromInvitationUrl(created.invitationUrl), adminEmail, tenantId);

  const members: Record<string, Actor> = {};
  for (const member of options.members ?? []) {
    const email = `${member.label}-${suffix}@example.com`;
    const { token } = await invite(admin, email, member.role);
    members[member.label] = await joinAsNewAccount(playwright, token, email, tenantId);
  }
  return { id: tenantId, slug, admin, members };
}

interface MemberItem {
  id: string;
  role: Role;
  status: string;
  account: { id: string; email: string };
}

/** Lists the admin's tenant members (every status) and returns the membership id of accountId. */
export async function membershipIdOf(admin: Actor, accountId: string): Promise<string> {
  for (const status of ['active', 'suspended', 'revoked']) {
    const page = await expectData<{ items: MemberItem[] }>(await admin.request.get(`/api/v1/memberships?status=${status}&size=100`), 200);
    const found = page.items.find((item) => item.account.id === accountId);
    if (found) {
      return found.id;
    }
  }
  assert.fail(`no membership of account ${accountId} in the admin tenant`);
}

let tenantlessCounter = 0;

/**
 * A signed-in actor with no membership: seeds a tenant with an admin and one member, the admin
 * revokes the member, the member's POST /auth/refresh answers 403 (cookie cleared), then
 * GET /auth/session re-issues it tenantless (the CA-04 steps).
 */
export async function tenantlessActor(playwright: Playwright, operator: Actor): Promise<Actor> {
  tenantlessCounter += 1;
  const label = `tenantless${tenantlessCounter}`;
  const tenant = await seedTenant(playwright, operator, { slugBase: `${label}-t`, members: [{ label, role: 'member' }] });
  const actor = tenant.members[label];
  assert.ok(actor, 'tenantlessActor: the member was not seeded');

  const membershipId = await membershipIdOf(tenant.admin, actor.accountId);
  await expectData<unknown>(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${membershipId}`, { status: 'revoked' }), 200);
  await expectError(await mutate(actor, 'POST', '/api/v1/auth/refresh'), 403, 'MEMBERSHIP_REVOKED');
  const session = await expectData<{ activeTenant: unknown; account: { role: unknown } }>(await actor.request.get('/api/v1/auth/session'), 200);
  assert.equal(session.activeTenant, null, 'tenantlessActor: the re-issued session still has a tenant');
  assert.equal(session.account.role, null, 'tenantlessActor: the re-issued session still has a role');
  await tenant.admin.request.dispose();
  return actor;
}

/** APP_BOOTSTRAP_TENANT_SLUG from the environment; asserts it is set. Used only by @tenancy scenarios. */
export function bootstrapTenantSlug(): string {
  const slug = process.env.APP_BOOTSTRAP_TENANT_SLUG;
  assert.ok(slug, 'APP_BOOTSTRAP_TENANT_SLUG is required');
  return slug;
}
