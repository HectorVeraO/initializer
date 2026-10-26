import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate, seedTenant } from '../../tck-support/actors';
import { defineParameter, expectView, overridePath, paramKey, readParameter } from './support';
import type { ParameterDeleted } from './support';

test('P4 a system-required parameter cannot be deleted', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const key = paramKey('protect.retry_attempts');
  await defineParameter(operator, { key, valueType: 'number', defaultValue: 3, minValue: 1, maxValue: 10, isSystemRequired: true });

  await expectError(await mutate(operator, 'DELETE', `/api/v1/parameters/${key}`), 409, 'SYSTEM_PARAMETER_PROTECTED');

  const stillListed = await readParameter(operator, key);
  expect(stillListed.isSystemRequired).toBe(true);
});

test('P5 deleting a definition removes its overrides', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p5-a' });
  const key = paramKey('custom.banner_text');
  await defineParameter(operator, { key, valueType: 'string', defaultValue: 'Hello' });
  await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'Hi' }), 200, key);

  const deleted = await expectData<ParameterDeleted>(await mutate(operator, 'DELETE', `/api/v1/parameters/${key}`), 200);
  expect(deleted).toEqual({ key, deletedOverrideCount: 1 });

  await defineParameter(operator, { key, valueType: 'string', defaultValue: 'Welcome' });
  const readA = await readParameter(tenantA.admin, key);
  expect([readA.effectiveValue, readA.source]).toEqual(['Welcome', 'global']);
});
