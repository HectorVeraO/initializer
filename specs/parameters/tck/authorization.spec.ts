import { test } from '@playwright/test';
import { activeTenantOf, bootstrapOperator, expectError, mutate } from '../../tck-support/actors';
import { defineParameter, overridePath, paramKey } from './support';

test('P11 an unknown key is not found', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  await expectError(await mutate(operator, 'DELETE', `/api/v1/parameters/${paramKey('missing')}`), 404, 'RESOURCE_NOT_FOUND');
  await operator.request.dispose();
});

test('P16 an override write without an active tenant answers 403 ACTIVE_TENANT_REQUIRED', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  test.skip((await activeTenantOf(operator)) !== null, 'the operator has a tenant: P16t pins this answer with tenancy');
  const key = paramKey('export.p16_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });

  await expectError(await mutate(operator, 'PUT', overridePath(key), { value: 5 }), 403, 'ACTIVE_TENANT_REQUIRED');
  await operator.request.dispose();
});
