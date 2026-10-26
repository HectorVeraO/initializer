import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate, seedTenant } from '../../tck-support/actors';
import { defineParameter, expectView, issueSet, overridePath, paramKey, rawMutate, readParameter } from './support';

test('P6 an override must match the type and the range', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p6-a' });
  const key = paramKey('export.max_rows_inline');
  await defineParameter(operator, { key, valueType: 'number', defaultValue: 50, minValue: 1, maxValue: 500 });

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

test('P7 a definition reports every problem at once', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const response = await mutate(operator, 'POST', '/api/v1/parameters', {
    key: 'Bad Key',
    valueType: 'number',
    defaultValue: 'x',
    minValue: 5,
    maxValue: 1,
  });
  const error = await expectError(response, 422, 'VALIDATION_FAILED');
  expect(issueSet(error)).toEqual([
    'defaultValue:PARAMETER_TYPE_MISMATCH',
    'key:PARAMETER_KEY_INVALID',
    'maxValue:PARAMETER_CONSTRAINT_INVALID',
  ]);
});

test('P8 an override must be an allowed value', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p8-a' });
  const key = paramKey('custom.theme');
  await defineParameter(operator, { key, valueType: 'string', defaultValue: 'light', allowedValues: ['light', 'dark'] });

  const refused = await expectError(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'blue' }), 422, 'VALIDATION_FAILED');
  expect(issueSet(refused)).toEqual(['value:PARAMETER_VALUE_NOT_ALLOWED']);
  const accepted = await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 'dark' }), 200, key);
  expect(accepted.effectiveValue).toBe('dark');
});

test('P9 new constraints cannot strand an existing override', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p9-a' });
  const key = paramKey('export.max_rows_inline');
  await defineParameter(operator, { key, valueType: 'number', defaultValue: 50, minValue: 1, maxValue: 500 });
  await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 400 }), 200, key);

  await expectError(
    await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, { defaultValue: 50, minValue: 1, maxValue: 100 }),
    409,
    'PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS',
  );

  const fresh = await readParameter(operator, key);
  expect(fresh.maxValue).toBe(500);
  const readA = await readParameter(tenantA.admin, key);
  expect([readA.effectiveValue, readA.source]).toEqual([400, 'tenant']);
});

test('P12 type and system flag are immutable, other fields are replaced', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, { slugBase: 'p12-a' });
  const key = paramKey('custom.retries_label');
  await defineParameter(operator, { key, valueType: 'string', defaultValue: 'x' });

  const refused = await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, { valueType: 'number', isSystemRequired: true, defaultValue: 'y' });
  const error = await expectError(refused, 422, 'VALIDATION_FAILED');
  expect(issueSet(error)).toEqual(['isSystemRequired:PARAMETER_FIELD_IMMUTABLE', 'valueType:PARAMETER_FIELD_IMMUTABLE']);

  const replaced = await expectView(await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, { defaultValue: 'y' }), 200, key);
  expect(replaced.defaultValue).toBe('y');
  const readA = await readParameter(tenantA.admin, key);
  expect([readA.effectiveValue, readA.source]).toEqual(['y', 'global']);
});

test('P13 transport problems answer their own codes', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  const malformed = await expectError(await rawMutate(operator, 'POST', '/api/v1/parameters', '{', 'application/json'), 422, 'VALIDATION_FAILED');
  expect(malformed.issues.map((issue) => ({ field: issue.field, code: issue.code }))).toEqual([{ field: '', code: 'BODY_MALFORMED' }]);

  const big = JSON.stringify({ description: 'a'.repeat(2 * 1024 * 1024) });
  await expectError(await rawMutate(operator, 'POST', '/api/v1/parameters', big, 'application/json'), 413, 'PAYLOAD_TOO_LARGE');

  await expectError(await rawMutate(operator, 'POST', '/api/v1/parameters', '{}', 'text/plain'), 415, 'UNSUPPORTED_MEDIA_TYPE');
});

test('P14 a method no route accepts is an unknown route', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  await expectError(await mutate(operator, 'PATCH', '/api/v1/parameters', {}), 404, 'ROUTE_NOT_FOUND');
  const page = await expectData<{ items: unknown[] }>(await operator.request.get('/api/v1/parameters?size=1'), 200);
  expect(Array.isArray(page.items)).toBe(true);
});
