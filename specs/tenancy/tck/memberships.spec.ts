import { strict as assert } from 'node:assert';
import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import {
  baseUrl,
  bootstrapOperator,
  expectData,
  expectError,
  mutate,
  runSuffix,
} from '../../tck-support/actors';
import { invite, membershipIdOf, seedTenant } from '../../tck-support/actors/tenants';
import type { Actor } from '../../tck-support/actors';

interface Member {
  id: string;
  role: string;
  status: string;
  account: { id: string; email: string };
}

interface TenantSummary {
  id: string;
  slug: string;
  name: string;
  status: string;
}

interface SessionView {
  account: { role: string | null };
  activeTenant: { slug: string } | null;
}

async function members(actor: Actor, query = ''): Promise<Member[]> {
  const page = await expectData<{ items: Member[] }>(await actor.request.get(`/api/v1/memberships${query}`), 200);
  return page.items;
}

async function currentTenant(actor: Actor): Promise<TenantSummary> {
  return expectData<TenantSummary>(await actor.request.get('/api/v1/tenant'), 200);
}

async function xsrfValue(actor: Actor): Promise<string> {
  const state = await actor.request.storageState();
  const cookie = state.cookies.find((item) => item.name === 'XSRF-TOKEN');
  assert.ok(cookie, 'the actor holds no XSRF-TOKEN cookie');
  return cookie.value;
}

test('CA-02 switch-tenant is stateless and refuses non-members', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca02-a', members: [{ label: 'ca02-u', role: 'member' }] });
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'ca02-b' });
  const tenantC = await seedTenant(playwright, operator, { slugBase: 'ca02-c' });
  const contextA = tenantA.members['ca02-u'];
  assert.ok(contextA, 'user U was not seeded');

  const { token } = await invite(tenantC.admin, contextA.email, 'viewer');
  const accepted = await mutate(contextA, 'POST', '/api/v1/invitations/accept', { token });
  await expectData<unknown>(accepted, 200);

  const requestB = await playwright.request.newContext({ baseURL: baseUrl(), storageState: await contextA.request.storageState() });
  const contextB: Actor = { ...contextA, request: requestB };

  const denied = await mutate(contextA, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenantB.id });
  await expectError(denied, 403, 'TENANT_ACCESS_DENIED');
  expect((await currentTenant(contextA)).slug).toBe(tenantA.slug);

  const switched = await mutate(contextA, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenantC.id });
  const session = await expectData<SessionView>(switched, 200);
  expect(session.activeTenant?.slug).toBe(tenantC.slug);
  expect(session.account.role).toBe('viewer');

  expect((await currentTenant(contextA)).slug).toBe(tenantC.slug);
  expect((await currentTenant(contextB)).slug).toBe(tenantA.slug);
  const listedByB = (await members(contextB)).map((member) => member.account.email);
  expect(listedByB).toContain(tenantA.admin.email);
  expect(listedByB).toContain(contextA.email);

  await requestB.dispose();
  for (const actor of [contextA, tenantA.admin, tenantB.admin, tenantC.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-05 the last active admin cannot be revoked or demoted', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca05-a' });
  const ownId = await membershipIdOf(tenant.admin, tenant.admin.accountId);

  await expectError(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${ownId}`, { status: 'revoked' }), 409, 'LAST_TENANT_ADMIN');
  await expectError(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${ownId}`, { role: 'member' }), 409, 'LAST_TENANT_ADMIN');

  const own = (await members(tenant.admin)).find((member) => member.id === ownId);
  expect(own?.role).toBe('admin');
  expect(own?.status).toBe('active');

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-05 membership updates accumulate validation issues', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca05-v' });
  const ownId = await membershipIdOf(tenant.admin, tenant.admin.accountId);

  const empty = await expectError(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${ownId}`, {}), 422, 'VALIDATION_FAILED');
  expect(empty.issues.map((issue) => [issue.field, issue.code])).toEqual([['', 'FIELD_REQUIRED']]);

  const bad = await expectError(
    await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${ownId}`, { role: 'owner', status: 3 }),
    422,
    'VALIDATION_FAILED',
  );
  expect(bad.issues.map((issue) => [issue.field, issue.code])).toEqual([
    ['role', 'FIELD_NOT_ALLOWED'],
    ['status', 'FIELD_TYPE_INVALID'],
  ]);

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-07 a revoked member returns only through a new invitation that keeps the membership', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca07-a', members: [{ label: 'ca07-jane', role: 'viewer' }] });
  const jane = tenant.members['ca07-jane'];
  assert.ok(jane, 'Jane was not seeded');
  const membershipId = await membershipIdOf(tenant.admin, jane.accountId);

  await expectError(await mutate(tenant.admin, 'POST', '/api/v1/invitations', { email: jane.email, role: 'viewer' }), 409, 'MEMBERSHIP_ALREADY_ACTIVE');

  await expectData<unknown>(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${membershipId}`, { status: 'revoked' }), 200);
  await expectError(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${membershipId}`, { status: 'active' }), 409, 'INVITATION_REQUIRED');

  const mixedCase = `Ca07-jane-${runSuffix()}@Example.com`;
  const { invitationId, token } = await invite(tenant.admin, mixedCase, 'member');
  const accepted = await expectData<{ membership: { id: string; status: string; role: string } }>(
    await mutate(jane, 'POST', '/api/v1/invitations/accept', { token }),
    200,
  );
  expect(accepted.membership.id).toBe(membershipId);
  expect(accepted.membership.status).toBe('active');
  expect(accepted.membership.role).toBe('member');

  await expectError(await mutate(tenant.admin, 'DELETE', `/api/v1/invitations/${invitationId}`), 409, 'INVITATION_NOT_PENDING');

  for (const actor of [jane, tenant.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-10 membership and invitation ids of another tenant answer the same 404 as a random or malformed id', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca10-a' });
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'ca10-b' });
  const foreignMembership = await membershipIdOf(tenantB.admin, tenantB.admin.accountId);
  const { invitationId: foreignInvitation } = await invite(tenantB.admin, `ca10-invitee-${runSuffix()}@example.com`, 'viewer');

  const attempts: Array<{ method: 'PATCH' | 'DELETE'; base: string; foreignId: string; body?: unknown }> = [
    { method: 'PATCH', base: '/api/v1/memberships', foreignId: foreignMembership, body: { status: 'revoked' } },
    { method: 'DELETE', base: '/api/v1/invitations', foreignId: foreignInvitation },
  ];
  for (const { method, base, foreignId, body } of attempts) {
    const foreign = await expectError(await mutate(tenantA.admin, method, `${base}/${foreignId}`, body), 404, 'RESOURCE_NOT_FOUND');
    const random = await expectError(await mutate(tenantA.admin, method, `${base}/${randomUUID()}`, body), 404, 'RESOURCE_NOT_FOUND');
    const malformed = await expectError(await mutate(tenantA.admin, method, `${base}/not-a-uuid`, body), 404, 'RESOURCE_NOT_FOUND');
    expect(foreign).toEqual(random);
    expect(foreign).toEqual(malformed);
  }

  const stillActive = (await members(tenantB.admin)).find((member) => member.id === foreignMembership);
  expect(stillActive?.status).toBe('active');

  for (const actor of [tenantA.admin, tenantB.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-13 a membership update without or with a wrong CSRF token is refused and changes nothing', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca13-a', members: [{ label: 'ca13-m', role: 'member' }] });
  const target = tenant.members['ca13-m'];
  assert.ok(target, 'the member was not seeded');
  const membershipId = await membershipIdOf(tenant.admin, target.accountId);
  const path = `/api/v1/memberships/${membershipId}`;
  const csrf = await xsrfValue(tenant.admin);

  const missing = await tenant.admin.request.fetch(path, { method: 'PATCH', data: { role: 'viewer' } });
  await expectError(missing, 403, 'CSRF_TOKEN_INVALID');
  const wrong = await tenant.admin.request.fetch(path, { method: 'PATCH', headers: { 'X-XSRF-TOKEN': `${csrf}x` }, data: { role: 'viewer' } });
  await expectError(wrong, 403, 'CSRF_TOKEN_INVALID');

  const unchanged = (await members(tenant.admin)).find((member) => member.id === membershipId);
  expect(unchanged?.role).toBe('member');

  for (const actor of [target, tenant.admin, operator]) {
    await actor.request.dispose();
  }
});

test('the member list pages, filters by role and never shows another tenant', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, {
    slugBase: 'ml-a',
    members: [
      { label: 'ml-viewer', role: 'viewer' },
      { label: 'ml-member', role: 'member' },
    ],
  });
  const other = await seedTenant(playwright, operator, { slugBase: 'ml-b' });
  const viewer = tenant.members['ml-viewer'];
  assert.ok(viewer, 'the viewer was not seeded');

  const all = await members(viewer);
  expect(all.map((member) => member.role).sort()).toEqual(['admin', 'member', 'viewer']);
  expect(all.map((member) => member.account.email)).not.toContain(other.admin.email);

  const onlyViewers = await members(tenant.admin, '?role=viewer');
  expect(onlyViewers.map((member) => member.account.id)).toEqual([viewer.accountId]);

  const secondPage = await expectData<{ items: Member[]; page: number; size: number; totalItems: number }>(
    await tenant.admin.request.get('/api/v1/memberships?size=2&page=2'),
    200,
  );
  expect(secondPage).toMatchObject({ page: 2, size: 2, totalItems: 3 });
  expect(secondPage.items).toHaveLength(1);

  const invalid = await expectError(await tenant.admin.request.get('/api/v1/memberships?status=pending&page=0'), 422, 'VALIDATION_FAILED');
  expect(invalid.issues.map((issue) => [issue.field, issue.code])).toEqual([
    ['status', 'FIELD_NOT_ALLOWED'],
    ['page', 'FIELD_OUT_OF_RANGE'],
  ]);

  const emailSort = await expectError(await tenant.admin.request.get('/api/v1/memberships?sort=email'), 422, 'VALIDATION_FAILED');
  expect(emailSort.issues.map((issue) => [issue.field, issue.code])).toEqual([['sort', 'FIELD_NOT_ALLOWED']]);

  for (const actor of [...Object.values(tenant.members), tenant.admin, other.admin, operator]) {
    await actor.request.dispose();
  }
});
