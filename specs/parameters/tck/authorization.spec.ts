import { test } from '@playwright/test';
import { bootstrapOperator, expectError, mutate } from '../../tck-support/actors';
import { seedTenant } from '../../tck-support/actors/tenants';
import { defineParameter, overridePath, paramKey } from './support';

test('P10 roles gate overrides and only operators define parameters', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p10-a', members: [{ label: 'p10-member', role: 'member' }] });
  const member = tenantA.members['p10-member'];
  const key = paramKey('export.p10_max_rows');
  await defineParameter(operator, { key, valueType: 'number', defaultValue: 50, minValue: 1, maxValue: 500 });

  await expectError(await mutate(member, 'PUT', overridePath(key), { value: 5 }), 403, 'INSUFFICIENT_ROLE');
  const valid = { key: paramKey('custom.admin_attempt'), valueType: 'string', defaultValue: 'x' };
  await expectError(await mutate(tenantA.admin, 'POST', '/api/v1/parameters', valid), 403, 'PLATFORM_OPERATOR_REQUIRED');
  await expectError(await mutate(tenantA.admin, 'DELETE', `/api/v1/parameters/${key}`), 403, 'PLATFORM_OPERATOR_REQUIRED');
});

test('P11 an unknown key is not found', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p11-a' });
  const missing = paramKey('missing');

  await expectError(await mutate(tenantA.admin, 'PUT', overridePath(missing), { value: 1 }), 404, 'RESOURCE_NOT_FOUND');
  await expectError(await mutate(operator, 'DELETE', `/api/v1/parameters/${missing}`), 404, 'RESOURCE_NOT_FOUND');
});
