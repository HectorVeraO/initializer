import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectError, mutate } from '../../../tck-support/actors';
import { seedTenant } from '../../../tck-support/actors/tenants';
import { defineParameter, expectView, issueSet, overridePath, paramKey, readParameter } from '../support';

test('P6 an override must match the type and the range', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p6-a',
  });
  const key = paramKey('export.p6_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });

  const cases: Array<[unknown, string]> = [
    [{ value: 'five' }, 'value:PARAMETER_TYPE_MISMATCH'],
    [{ value: 0 }, 'value:PARAMETER_OUT_OF_RANGE'],
    [{ value: 501 }, 'value:PARAMETER_OUT_OF_RANGE'],
    [{}, 'value:FIELD_REQUIRED'],
  ];
  for (const [body, issue] of cases) {
    const error = await expectError(await mutate(tenantA.admin, 'PUT', overridePath(key), body), 422, 'VALIDATION_FAILED');
    expect(issueSet(error)).toEqual([issue]);
  }
});

test('P8 an override must be an allowed value', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p8-a',
  });
  const key = paramKey('custom.theme');
  await defineParameter(operator, {
    key,
    valueType: 'string',
    defaultValue: 'light',
    allowedValues: ['light', 'dark'],
  });

  const refused = await expectError(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'blue' }), 422, 'VALIDATION_FAILED');
  expect(issueSet(refused)).toEqual(['value:PARAMETER_VALUE_NOT_ALLOWED']);
  const accepted = await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'dark' }), 200, key);
  expect(accepted.effectiveValue).toBe('dark');
});

test('P9 new constraints cannot strand an existing override', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p9-a',
  });
  const key = paramKey('export.p9_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });
  await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 400 }), 200, key);

  await expectError(
    await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, {
      defaultValue: 50,
      minValue: 1,
      maxValue: 100,
    }),
    409,
    'PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS',
  );

  const fresh = await readParameter(operator, key);
  expect(fresh.maxValue).toBe(500);
  const readA = await readParameter(tenantA.admin, key);
  expect([readA.effectiveValue, readA.source]).toEqual([400, 'tenant']);
});

test('P15 a value equal to a decimal bound is inside the range', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p15-a',
  });
  const key = paramKey('retry.p15_backoff');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 0.1,
    minValue: 0.1,
    maxValue: 0.3,
  });

  for (const value of [0.1, 0.3]) {
    const accepted = await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value }), 200, key);
    expect(accepted.effectiveValue).toBe(value);
  }
  for (const value of [0.09, 0.31]) {
    const error = await expectError(await mutate(tenantA.admin, 'PUT', overridePath(key), { value }), 422, 'VALIDATION_FAILED');
    expect(issueSet(error)).toEqual(['value:PARAMETER_OUT_OF_RANGE']);
  }
});
