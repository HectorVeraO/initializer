import { randomUUID } from 'node:crypto';
import { test } from '@playwright/test';
import { bootstrapOperator, expectError, mutate } from '../../tck-support/actors';
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
