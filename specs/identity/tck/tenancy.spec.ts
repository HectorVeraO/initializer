import { strict as assert } from 'node:assert';
import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate } from '../../tck-support/actors';
import { membershipIdOf, seedTenant, tenantlessActor } from '../../tck-support/actors/tenants';

interface TenantSummary {
  id: string;
  slug: string;
  name: string;
  status: string;
}

interface SessionView {
  account: { role: string | null };
  activeTenant: TenantSummary | null;
}

test('GET /tenant answers the caller tenant for its id or slug in any case and refuses another tenant', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'tn-a' });
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'tn-b' });

  const own = await expectData<TenantSummary>(await tenantA.admin.request.get('/api/v1/tenant'), 200);
  expect(own).toMatchObject({ id: tenantA.id, slug: tenantA.slug, status: 'active' });

  for (const reference of [tenantA.slug.toUpperCase(), tenantA.id.toUpperCase(), tenantA.slug, tenantA.id]) {
    const answer = await expectData<TenantSummary>(await tenantA.admin.request.get(`/api/v1/tenant?tenantId=${reference}`), 200);
    expect(answer.slug).toBe(tenantA.slug);
  }
  for (const reference of [tenantB.slug, tenantB.id, randomUUID(), 'not-a-tenant']) {
    await expectError(await tenantA.admin.request.get(`/api/v1/tenant?tenantId=${reference}`), 403, 'TENANT_MISMATCH');
  }

  for (const actor of [tenantA.admin, tenantB.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-14 GET /tenant refuses a missing or altered session cookie', { tag: '@identity' }, async ({ playwright, request }) => {
  await expectError(await request.get('/api/v1/tenant'), 401, 'SESSION_INVALID');

  const operator = await bootstrapOperator(playwright);
  const state = await operator.request.storageState();
  const sessionCookie = state.cookies.find((cookie) => cookie.httpOnly && cookie.name !== 'XSRF-TOKEN' && !cookie.name.startsWith('ory_'));
  assert.ok(sessionCookie, 'the actor holds no BFF session cookie');
  const middle = Math.floor(sessionCookie.value.length / 2);
  const replacement = sessionCookie.value[middle] === 'A' ? 'B' : 'A';
  const altered = sessionCookie.value.slice(0, middle) + replacement + sessionCookie.value.slice(middle + 1);

  const tampered = await playwright.request.newContext({
    baseURL: process.env.TCK_BASE_URL,
    extraHTTPHeaders: { Cookie: `${sessionCookie.name}=${altered}` },
  });
  await expectError(await tampered.get('/api/v1/tenant'), 401, 'SESSION_INVALID');
  await tampered.dispose();
  await operator.request.dispose();
});

test('CA-04 a revoked membership blocks refresh and switch, and a suspended tenant blocks the switch', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca04-a', members: [{ label: 'ca04-u', role: 'member' }] });
  const user = tenant.members['ca04-u'];
  assert.ok(user, 'user U was not seeded');
  const membershipId = await membershipIdOf(tenant.admin, user.accountId);

  await expectData<unknown>(await mutate(tenant.admin, 'PATCH', `/api/v1/memberships/${membershipId}`, { status: 'revoked' }), 200);

  await expectError(await mutate(user, 'POST', '/api/v1/auth/refresh'), 403, 'MEMBERSHIP_REVOKED');
  await expectError(await user.request.get('/api/v1/tenant'), 401, 'SESSION_INVALID');

  const reissued = await expectData<SessionView>(await user.request.get('/api/v1/auth/session'), 200);
  expect(reissued.activeTenant).toBeNull();
  expect(reissued.account.role).toBeNull();

  await expectError(await mutate(user, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenant.id }), 403, 'MEMBERSHIP_REVOKED');

  await expectData<unknown>(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'suspended' }), 200);
  await expectError(await mutate(user, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenant.id }), 403, 'TENANT_SUSPENDED');

  for (const actor of [user, tenant.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-11 role and operator codes on the tenancy routes', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca11-a', members: [{ label: 'ca11-m', role: 'member' }] });
  const member = tenant.members['ca11-m'];
  assert.ok(member, 'the member was not seeded');
  const tenantless = await tenantlessActor(playwright, operator);

  await expectError(await member.request.get('/api/v1/invitations'), 403, 'INSUFFICIENT_ROLE');
  await expectError(await tenant.admin.request.get('/api/v1/tenants'), 403, 'PLATFORM_OPERATOR_REQUIRED');
  await expectError(await mutate(tenant.admin, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'suspended' }), 403, 'PLATFORM_OPERATOR_REQUIRED');
  await expectError(await tenantless.request.get('/api/v1/memberships'), 403, 'ACTIVE_TENANT_REQUIRED');
  await expectError(await mutate(member, 'PATCH', `/api/v1/memberships/${randomUUID()}`, { role: 'viewer' }), 403, 'INSUFFICIENT_ROLE');

  for (const actor of [tenantless, member, tenant.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-16 a signed-in user without a membership gets the same refusal for a suspended and an unknown tenant', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const suspended = await seedTenant(playwright, operator, { slugBase: 'ca16-s' });
  await expectData<unknown>(await mutate(operator, 'PATCH', `/api/v1/tenants/${suspended.id}/status`, { status: 'suspended' }), 200);
  const tenantless = await tenantlessActor(playwright, operator);

  const toSuspended = await expectError(
    await mutate(tenantless, 'POST', '/api/v1/auth/switch-tenant', { tenantId: suspended.id }),
    403,
    'TENANT_ACCESS_DENIED',
  );
  const toUnknown = await expectError(
    await mutate(tenantless, 'POST', '/api/v1/auth/switch-tenant', { tenantId: randomUUID() }),
    403,
    'TENANT_ACCESS_DENIED',
  );
  expect(toSuspended).toEqual(toUnknown);

  for (const actor of [tenantless, suspended.admin, operator]) {
    await actor.request.dispose();
  }
});

test('the operator lists, filters and toggles tenant status', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ts-a' });

  const suspended = await expectData<TenantSummary>(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'suspended' }), 200);
  expect(suspended).toMatchObject({ id: tenant.id, status: 'suspended' });
  const again = await expectData<TenantSummary>(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'suspended' }), 200);
  expect(again.status).toBe('suspended');

  const listed = await expectData<{ items: TenantSummary[]; totalItems: number }>(await operator.request.get('/api/v1/tenants?status=suspended&size=100&sort=-createdAt'), 200);
  expect(listed.items.map((item) => item.id)).toContain(tenant.id);
  expect(listed.items.every((item) => item.status === 'suspended')).toBe(true);

  await expectData<unknown>(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'active' }), 200);
  const active = await expectData<{ items: TenantSummary[] }>(await operator.request.get('/api/v1/tenants?status=active&size=100&sort=-createdAt'), 200);
  expect(active.items.map((item) => item.id)).toContain(tenant.id);

  const invalid = await expectError(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'revoked' }), 422, 'VALIDATION_FAILED');
  expect(invalid.issues.map((issue) => [issue.field, issue.code])).toEqual([['status', 'FIELD_NOT_ALLOWED']]);
  const unknown = await expectError(await mutate(operator, 'PATCH', `/api/v1/tenants/${randomUUID()}/status`, { status: 'active' }), 404, 'RESOURCE_NOT_FOUND');
  const malformed = await expectError(await mutate(operator, 'PATCH', '/api/v1/tenants/not-a-uuid/status', { status: 'active' }), 404, 'RESOURCE_NOT_FOUND');
  expect(unknown).toEqual(malformed);

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});
