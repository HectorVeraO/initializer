import { strict as assert } from 'node:assert';
import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { APIResponse } from '@playwright/test';
import { apiKeyClient, bootstrapOperator, expectData, expectError, mutate, seedTenant } from '../../tck-support/actors';
import type { Actor } from '../../tck-support/actors';

interface ApiKeyView {
  id: string;
  name: string;
  keyPrefix: string;
  scopes: string[];
  status: string;
  expiresAt: string | null;
  revokedAt: string | null;
}

interface ApiKeyCreated {
  apiKey: ApiKeyView;
  plainTextKey: string;
}

interface TenantSummary {
  id: string;
  slug: string;
}

const KEY_PATTERN = /^gbk_live_[a-z2-7]{16}_[a-z2-7]{52}$/;
const PREFIX_PATTERN = /^gbk_live_[a-z2-7]{16}$/;
const BASE32 = 'abcdefghijklmnopqrstuvwxyz234567';

/** Random lower-case base32 characters; keys are built at run time, never written as literals. */
function randomBase32(length: number): string {
  return Array.from(randomBytes(length), (byte) => BASE32[byte % BASE32.length]).join('');
}

async function createKey(admin: Actor, name: string, scopes: string[], expiresAt: string | null = null): Promise<ApiKeyCreated> {
  const created = await expectData<ApiKeyCreated>(await mutate(admin, 'POST', '/api/v1/api-keys', { name, scopes, expiresAt }), 201);
  expect(created.plainTextKey).toMatch(KEY_PATTERN);
  return created;
}

async function rawPost(admin: Actor, path: string, body: string): Promise<APIResponse> {
  const state = await admin.request.storageState();
  const csrf = state.cookies.find((cookie) => cookie.name === 'XSRF-TOKEN');
  assert.ok(csrf, 'the actor holds no XSRF-TOKEN cookie');
  return admin.request.fetch(path, { method: 'POST', headers: { 'X-XSRF-TOKEN': csrf.value, 'Content-Type': 'application/json' }, data: body });
}

test('CA-03 API key listings never expose secrets and a key lists only with its scope', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca03-a' });
  const oneHourAgo = new Date(Date.now() - 3_600_000).toISOString();

  const kTenant = await createKey(tenantA.admin, 'k-tenant', ['tenant:read']);
  const kBoth = await createKey(tenantA.admin, 'k-both', ['tenant:read', 'api-keys:read']);
  const kExpired = await createKey(tenantA.admin, 'k-expired', ['tenant:read'], oneHourAgo);
  const plainKeys = [kTenant, kBoth, kExpired].map((created) => created.plainTextKey);

  const adminListing = await tenantA.admin.request.get('/api/v1/api-keys?size=100');
  const adminText = await adminListing.text();
  const adminPage = await expectData<{ items: ApiKeyView[] }>(adminListing, 200);
  expect(adminPage.items.length).toBeGreaterThanOrEqual(3);
  for (const item of adminPage.items) {
    expect(item.keyPrefix).toMatch(PREFIX_PATTERN);
  }
  for (const plain of plainKeys) {
    expect(adminText).not.toContain(plain);
    expect(adminText).not.toContain(plain.slice(-52));
  }

  const narrow = await apiKeyClient(playwright, kTenant.plainTextKey);
  await expectError(await narrow.get('/api/v1/api-keys'), 403, 'INSUFFICIENT_SCOPE');

  const both = await apiKeyClient(playwright, kBoth.plainTextKey);
  const keyListing = await both.get('/api/v1/api-keys?size=100');
  const keyText = await keyListing.text();
  const keyPage = await expectData<{ items: ApiKeyView[] }>(keyListing, 200);
  const prefixes = keyPage.items.map((item) => item.keyPrefix);
  for (const created of [kTenant, kBoth, kExpired]) {
    expect(prefixes).toContain(created.apiKey.keyPrefix);
  }
  for (const plain of plainKeys) {
    expect(keyText).not.toContain(plain);
    expect(keyText).not.toContain(plain.slice(-52));
  }

  for (const client of [narrow, both]) {
    await client.dispose();
  }
  for (const actor of [tenantA.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-03 a key resolves its tenant by slug or id and rejects another tenant', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca03t-a' });
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'ca03t-b' });
  const key = await createKey(tenantA.admin, 'k-tenant-ref', ['tenant:read']);
  const client = await apiKeyClient(playwright, key.plainTextKey);

  for (const reference of [tenantA.slug, tenantA.id.toUpperCase()]) {
    const summary = await expectData<TenantSummary>(await client.get(`/api/v1/tenant?tenantId=${reference}`), 200);
    expect(summary.slug).toBe(tenantA.slug);
  }
  await expectError(await client.get(`/api/v1/tenant?tenantId=${tenantB.slug}`), 403, 'TENANT_MISMATCH');

  await client.dispose();
  for (const actor of [tenantA.admin, tenantB.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-03 expired, revoked, unknown, malformed and wrong-secret keys answer the same 401', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca03i-a' });
  const oneHourAgo = new Date(Date.now() - 3_600_000).toISOString();
  const kActive = await createKey(tenantA.admin, 'k-active', ['tenant:read']);
  const kRevoked = await createKey(tenantA.admin, 'k-revoked', ['tenant:read']);
  const kExpired = await createKey(tenantA.admin, 'k-expired', ['tenant:read'], oneHourAgo);

  const expiredClient = await apiKeyClient(playwright, kExpired.plainTextKey);
  const expiredError = await expectError(await expiredClient.get('/api/v1/tenant'), 401, 'API_KEY_INVALID_OR_REVOKED');
  await expiredClient.dispose();

  const revoked = await expectData<ApiKeyView>(await mutate(tenantA.admin, 'DELETE', `/api/v1/api-keys/${kRevoked.apiKey.id}`), 200);
  expect(revoked.status).toBe('revoked');
  expect(revoked.revokedAt).not.toBeNull();

  const unknownKey = `gbk_live_${randomBase32(16)}_${randomBase32(52)}`;
  const malformedKey = `gbk_live_${randomBase32(8)}`;
  const wrongSecret = `${kActive.apiKey.keyPrefix}_${randomBase32(52)}`;
  for (const key of [kRevoked.plainTextKey, unknownKey, malformedKey, wrongSecret]) {
    const client = await apiKeyClient(playwright, key);
    const error = await expectError(await client.get('/api/v1/tenant'), 401, 'API_KEY_INVALID_OR_REVOKED');
    expect(error).toEqual(expiredError);
    await client.dispose();
  }

  for (const actor of [tenantA.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-04 a key answers TENANT_SUSPENDED on the request after its tenant is suspended', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca04k-a' });
  const key = await createKey(tenant.admin, 'k-suspend', ['tenant:read']);
  const client = await apiKeyClient(playwright, key.plainTextKey);

  await expectData<TenantSummary>(await client.get('/api/v1/tenant'), 200);
  await expectData<unknown>(await mutate(operator, 'PATCH', `/api/v1/tenants/${tenant.id}/status`, { status: 'suspended' }), 200);

  await expectError(await client.get('/api/v1/tenant'), 403, 'TENANT_SUSPENDED');

  await client.dispose();
  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-05 POST /api/v1/api-keys accumulates every issue', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca05k-a' });
  const issues = async (response: APIResponse) => (await expectError(response, 422, 'VALIDATION_FAILED')).issues.map((issue) => [issue.field, issue.code]);

  expect(await issues(await mutate(tenant.admin, 'POST', '/api/v1/api-keys', { name: '', scopes: [] }))).toEqual([
    ['name', 'FIELD_REQUIRED'],
    ['scopes', 'FIELD_REQUIRED'],
  ]);
  expect(await issues(await mutate(tenant.admin, 'POST', '/api/v1/api-keys', { name: 5, scopes: 'x', expiresAt: 'soon' }))).toEqual([
    ['name', 'FIELD_TYPE_INVALID'],
    ['scopes', 'FIELD_TYPE_INVALID'],
    ['expiresAt', 'FIELD_INVALID_FORMAT'],
  ]);
  expect(await issues(await mutate(tenant.admin, 'POST', '/api/v1/api-keys', { name: 'a', scopes: ['tenant:read', 'nope:nope', 'tenant:read'] }))).toEqual([
    ['scopes[1]', 'FIELD_NOT_ALLOWED'],
    ['scopes[2]', 'FIELD_DUPLICATE'],
  ]);
  expect(await issues(await rawPost(tenant.admin, '/api/v1/api-keys', '{"name":'))).toEqual([['', 'BODY_MALFORMED']]);

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-10 an API key id of another tenant, an unknown one and a malformed one answer the same 404', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca10k-a' });
  const tenantB = await seedTenant(playwright, operator, { slugBase: 'ca10k-b' });
  const foreignKey = await createKey(tenantB.admin, 'foreign', ['tenant:read']);

  const foreign = await expectError(await mutate(tenantA.admin, 'DELETE', `/api/v1/api-keys/${foreignKey.apiKey.id}`), 404, 'RESOURCE_NOT_FOUND');
  const unknown = await expectError(await mutate(tenantA.admin, 'DELETE', `/api/v1/api-keys/${crypto.randomUUID()}`), 404, 'RESOURCE_NOT_FOUND');
  const malformed = await expectError(await mutate(tenantA.admin, 'DELETE', '/api/v1/api-keys/not-a-uuid'), 404, 'RESOURCE_NOT_FOUND');
  expect(unknown).toEqual(foreign);
  expect(malformed).toEqual(foreign);

  const stillActive = await expectData<{ items: ApiKeyView[] }>(await tenantB.admin.request.get('/api/v1/api-keys?status=active'), 200);
  expect(stillActive.items.some((item) => item.id === foreignKey.apiKey.id)).toBe(true);

  for (const actor of [tenantA.admin, tenantB.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-11 a viewer cannot create keys and a key cannot reach session-only routes', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca11k-a', members: [{ label: 'ca11k-viewer', role: 'viewer' }] });
  const viewer = tenant.members['ca11k-viewer'];
  assert.ok(viewer, 'the viewer was not seeded');

  await expectError(await mutate(viewer, 'POST', '/api/v1/api-keys', { name: 'a', scopes: ['tenant:read'] }), 403, 'INSUFFICIENT_ROLE');

  const key = await createKey(tenant.admin, 'k-ca11', ['tenant:read']);
  const client = await apiKeyClient(playwright, key.plainTextKey);
  await expectError(await client.post('/api/v1/invitations', { data: { email: 'a@example.com', role: 'viewer' } }), 401, 'SESSION_INVALID');
  await expectError(await client.get('/api/v1/memberships'), 401, 'SESSION_INVALID');

  await client.dispose();
  for (const actor of [viewer, tenant.admin, operator]) {
    await actor.request.dispose();
  }
});

test('CA-14 a session cookie together with X-API-Key is a credential conflict', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'ca14-a' });
  const key = await createKey(tenant.admin, 'k-ca14', ['tenant:read']);

  const response = await tenant.admin.request.get('/api/v1/tenant', { headers: { 'X-API-Key': key.plainTextKey } });

  await expectError(response, 409, 'CREDENTIAL_CONFLICT');
  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-15 creating a key with a changed active tenant answers ACTIVE_TENANT_CHANGED and stores nothing', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'ca15k-a' });
  const tenantC = await seedTenant(playwright, operator, { slugBase: 'ca15k-c' });
  const state = await tenantA.admin.request.storageState();
  const csrf = state.cookies.find((cookie) => cookie.name === 'XSRF-TOKEN');
  assert.ok(csrf, 'the admin holds no XSRF-TOKEN cookie');
  const post = (activeTenantId: string) =>
    tenantA.admin.request.fetch('/api/v1/api-keys', {
      method: 'POST',
      headers: { 'X-XSRF-TOKEN': csrf.value, 'X-Active-Tenant-Id': activeTenantId },
      data: { name: 'k-ca15', scopes: ['tenant:read'] },
    });

  await expectError(await post(tenantC.id), 409, 'ACTIVE_TENANT_CHANGED');
  const before = await expectData<{ items: ApiKeyView[] }>(await tenantA.admin.request.get('/api/v1/api-keys?size=100'), 200);
  expect(before.items.some((item) => item.name === 'k-ca15')).toBe(false);

  await expectData<ApiKeyCreated>(await post(tenantA.id), 201);

  for (const actor of [tenantA.admin, tenantC.admin, operator]) {
    await actor.request.dispose();
  }
});
