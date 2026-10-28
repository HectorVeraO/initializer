import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { activeTenantOf, bootstrapOperator, expectData, expectError, mutate } from '../../tck-support/actors';
import { apiKeyClient, wellFormedFakeKey } from '../../tck-support/actors/api-keys';

test('CA-14 a session cookie together with X-API-Key is a credential conflict', { tag: '@api-keys' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  // The guard checks that both credentials are present before it resolves either.
  const response = await operator.request.get('/api/v1/api-keys', {
    headers: { 'X-API-Key': wellFormedFakeKey() },
  });

  await expectError(response, 409, 'CREDENTIAL_CONFLICT');
  await operator.request.dispose();
});

test('K1 an unknown or malformed key answers 401 API_KEY_INVALID_OR_REVOKED', { tag: '@api-keys' }, async ({ playwright }) => {
  const unknownKey = wellFormedFakeKey();
  const lastSeparator = unknownKey.lastIndexOf('_');
  const malformedKey = `${unknownKey.slice(0, lastSeparator)}.${unknownKey.slice(lastSeparator + 1)}`;

  for (const key of [unknownKey, malformedKey]) {
    const client = await apiKeyClient(playwright, key);
    await expectError(await client.get('/api/v1/api-keys'), 401, 'API_KEY_INVALID_OR_REVOKED');
    await client.dispose();
  }
});

test('K3 an unknown or malformed key id answers 404', { tag: '@api-keys' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  await expectError(await mutate(operator, 'DELETE', `/api/v1/api-keys/${randomUUID()}`), 404, 'RESOURCE_NOT_FOUND');
  await expectError(await mutate(operator, 'DELETE', '/api/v1/api-keys/not-a-uuid'), 404, 'RESOURCE_NOT_FOUND');
  await operator.request.dispose();
});

interface ScopeInfo {
  name: string;
  description: string;
  level: string;
}

const SCOPE_LEVELS = ['tenant', 'any'];

test('K2 the scope list is sorted by name and gives each scope a level', { tag: '@api-keys' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  const scopes = await expectData<ScopeInfo[]>(await operator.request.get('/api/v1/api-key-scopes'), 200);

  const names = scopes.map((scope) => scope.name);
  expect(names).toEqual([...names].sort());
  expect(names).toContain('api-keys:read');
  expect(scopes.find((scope) => scope.name === 'api-keys:read')?.level).toBe('tenant');
  for (const scope of scopes) {
    expect(SCOPE_LEVELS).toContain(scope.level);
  }
  await operator.request.dispose();
});

test('K4 a key without a tenant cannot hold a tenant-level scope', { tag: '@api-keys' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  // Only an operator without an active tenant mints a key without a tenant.
  test.skip((await activeTenantOf(operator)) !== null, 'the operator has a tenant: it mints keys with a tenant');
  const scopes = await expectData<ScopeInfo[]>(await operator.request.get('/api/v1/api-key-scopes'), 200);
  const tenantLevel = scopes.find((scope) => scope.level === 'tenant');
  test.skip(tenantLevel === undefined, 'no tenant-level scope is registered');

  const error = await expectError(
    await mutate(operator, 'POST', '/api/v1/api-keys', { name: `k4-${randomUUID().slice(0, 8)}`, scopes: [tenantLevel?.name] }),
    422,
    'VALIDATION_FAILED',
  );

  expect(error.issues.map((issue) => [issue.field, issue.code])).toEqual([['scopes[0]', 'SCOPE_LEVEL_MISMATCH']]);
  await operator.request.dispose();
});
