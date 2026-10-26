import { strict as assert } from 'node:assert';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, APIResponse } from '@playwright/test';
import {
  SEEDED_PASSWORD,
  bootstrapOperator,
  expectData,
  expectError,
  invite,
  login,
  mutate,
  runSuffix,
  seedTenant,
  tokenFromInvitationUrl,
} from '../../tck-support/actors';
import type { Actor } from '../../tck-support/actors';

interface InvitationItem {
  id: string;
  email: string;
  role: string;
  status: string;
}

interface Membership {
  role: string;
  status: string;
}

const publicOrigin = (): string => process.env.GATBASE_PUBLIC_BASE_URL ?? process.env.TCK_BASE_URL ?? '';

async function acceptNew(anonymous: APIRequestContext, body: unknown): Promise<APIResponse> {
  return anonymous.post('/api/v1/invitations/accept-new', { data: body });
}

async function listInvitations(admin: Actor, status = 'pending'): Promise<InvitationItem[]> {
  const page = await expectData<{ items: InvitationItem[] }>(await admin.request.get(`/api/v1/invitations?status=${status}`), 200);
  return page.items;
}

async function rawPost(admin: Actor, body: string, contentType: string): Promise<APIResponse> {
  const state = await admin.request.storageState();
  const csrf = state.cookies.find((cookie) => cookie.name === 'XSRF-TOKEN');
  assert.ok(csrf, 'the actor holds no XSRF-TOKEN cookie');
  return admin.request.fetch('/api/v1/invitations', {
    method: 'POST',
    headers: { 'X-XSRF-TOKEN': csrf.value, 'Content-Type': contentType },
    data: body,
  });
}

test('CA-06 invitations carry a fragment token, match the email and are single use', { tag: '@core-auth' }, async ({ playwright, request }) => {
  const suffix = runSuffix();
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca06-a' });
  const other = await seedTenant(playwright, operator, { slugBase: 'ca06-w' });
  const janeEmail = `jane-${suffix}@example.com`;

  const created = await expectData<{ invitation: InvitationItem; invitationUrl: string }>(
    await mutate(tenantA.admin, 'POST', '/api/v1/invitations', { email: janeEmail, role: 'viewer' }),
    201,
  );
  expect(created.invitationUrl.startsWith(`${publicOrigin()}/invitations/accept#token=`)).toBe(true);
  expect(new URL(created.invitationUrl).search).toBe('');
  const token = tokenFromInvitationUrl(created.invitationUrl);

  const listing = await tenantA.admin.request.get('/api/v1/invitations');
  const listed = (await expectData<{ items: Array<Record<string, unknown>> }>(listing, 200)).items;
  const item = listed.find((entry) => entry.id === created.invitation.id);
  expect(item?.status).toBe('pending');
  expect(Object.keys(item ?? {}).some((key) => /token|url|hash/i.test(key))).toBe(false);
  expect(await listing.text()).not.toContain(token);

  const mismatch = await mutate(other.admin, 'POST', '/api/v1/invitations/accept', { token });
  await expectError(mismatch, 403, 'INVITATION_EMAIL_MISMATCH');
  const stillPending = (await listInvitations(tenantA.admin)).some((entry) => entry.id === created.invitation.id && entry.status === 'pending');
  expect(stillPending).toBe(true);

  const joined = await expectData<{ membership: Membership }>(
    await acceptNew(request, { token, password: SEEDED_PASSWORD, fullName: 'Jane Doe' }),
    201,
  );
  expect(joined.membership.role).toBe('viewer');
  expect(joined.membership.status).toBe('active');

  const jane = await login(playwright, `Jane-${suffix}@Example.com`, SEEDED_PASSWORD);
  const session = await expectData<{ memberships: Array<{ tenant: { slug: string } }> }>(await jane.request.get('/api/v1/auth/session'), 200);
  expect(session.memberships.some((membership) => membership.tenant.slug === tenantA.slug)).toBe(true);

  await expectError(await mutate(other.admin, 'POST', '/api/v1/invitations/accept', { token }), 404, 'INVITATION_INVALID');
  await expectError(await acceptNew(request, { token, password: SEEDED_PASSWORD, fullName: 'Jane Doe' }), 404, 'INVITATION_INVALID');

  for (const actor of [jane, tenantA.admin, other.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-08 a new invitation supersedes the pending one', { tag: '@core-auth' }, async ({ playwright, request }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca08-a' });
  const email = `ca08-${runSuffix()}@example.com`;

  const first = await invite(tenant.admin, email, 'member');
  const second = await invite(tenant.admin, email, 'member');

  const body = { password: SEEDED_PASSWORD, fullName: 'Jane Doe' };
  await expectError(await acceptNew(request, { token: first.token, ...body }), 404, 'INVITATION_INVALID');
  await expectData<unknown>(await acceptNew(request, { token: second.token, ...body }), 201);

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-09 accept-new accumulates issues and leaves an existing identity to accept', { tag: '@core-auth' }, async ({ playwright, request }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'ca09-b' });
  const other = await seedTenant(playwright, operator, { slugBase: 'ca09-w' });

  const invalid = await expectError(await acceptNew(request, { token: 'short', password: 'short', fullName: '  ' }), 422, 'VALIDATION_FAILED');
  expect(invalid.issues.map((issue) => [issue.field, issue.code])).toEqual([
    ['token', 'FIELD_INVALID_FORMAT'],
    ['password', 'FIELD_TOO_SHORT'],
    ['fullName', 'FIELD_REQUIRED'],
  ]);

  const { token } = await invite(tenantB.admin, other.admin.email, 'member');
  await expectError(await acceptNew(request, { token, password: SEEDED_PASSWORD, fullName: 'W User' }), 409, 'IDENTITY_ALREADY_EXISTS');
  await expectData<unknown>(await mutate(other.admin, 'POST', '/api/v1/invitations/accept', { token }), 200);

  for (const actor of [tenantB.admin, other.admin, operator]) {
    await actor.request.dispose();
  }
});

test('Transport limits on POST /api/v1/invitations answer the registry codes', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'tl-a' });

  const malformed = await expectError(await rawPost(tenant.admin, '{"email":', 'application/json'), 422, 'VALIDATION_FAILED');
  expect(malformed.issues.map((issue) => [issue.field, issue.code])).toEqual([['', 'BODY_MALFORMED']]);

  const oversized = JSON.stringify({ email: 'a'.repeat(1024 * 1024 + 1) });
  await expectError(await rawPost(tenant.admin, oversized, 'application/json'), 413, 'PAYLOAD_TOO_LARGE');

  const plain = JSON.stringify({ email: `plain-${runSuffix()}@example.com`, role: 'member' });
  await expectError(await rawPost(tenant.admin, plain, 'text/plain'), 415, 'UNSUPPORTED_MEDIA_TYPE');

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});
