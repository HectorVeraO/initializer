import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, mutate } from '../../../tck-support/actors';
import { seedTenant } from '../../../tck-support/actors/tenants';
import { defineParameter, expectView, overridePath, paramKey, readParameter } from '../support';
import type { ParameterDeleted } from '../support';

test('P5 deleting a definition removes its overrides', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p5-a',
  });
  const key = paramKey('custom.banner_text');
  await defineParameter(operator, {
    key,
    valueType: 'string',
    defaultValue: 'Hello',
  });
  await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'Hi' }), 200, key);

  const deleted = await expectData<ParameterDeleted>(await mutate(operator, 'DELETE', `/api/v1/parameters/${key}`), 200);
  expect(deleted).toEqual({ key, deletedOverrideCount: 1 });

  await defineParameter(operator, {
    key,
    valueType: 'string',
    defaultValue: 'Welcome',
  });
  const readA = await readParameter(tenantA.admin, key);
  expect([readA.effectiveValue, readA.source]).toEqual(['Welcome', 'global']);
});
