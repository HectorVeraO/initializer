import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate } from '../../tck-support/actors';
import { defineParameter, expectView, issueSet, paramKey, rawMutate, readParameter } from './support';

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
  expect(issueSet(error)).toEqual(['defaultValue:PARAMETER_TYPE_MISMATCH', 'key:PARAMETER_KEY_INVALID', 'maxValue:PARAMETER_CONSTRAINT_INVALID']);
});

test('P12 type and system flag are immutable, other fields are replaced', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const key = paramKey('custom.retries_label');
  await defineParameter(operator, {
    key,
    valueType: 'string',
    defaultValue: 'x',
  });

  const refused = await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, {
    valueType: 'number',
    isSystemRequired: true,
    defaultValue: 'y',
  });
  const error = await expectError(refused, 422, 'VALIDATION_FAILED');
  expect(issueSet(error)).toEqual(['isSystemRequired:PARAMETER_FIELD_IMMUTABLE', 'valueType:PARAMETER_FIELD_IMMUTABLE']);

  const replaced = await expectView(
    await mutate(operator, 'PUT', `/api/v1/parameters/${key}`, {
      defaultValue: 'y',
    }),
    200,
    key,
  );
  expect(replaced.defaultValue).toBe('y');
  const read = await readParameter(operator, key);
  expect([read.effectiveValue, read.source]).toEqual(['y', 'global']);
});

test('P13 transport problems answer their own codes', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  const malformed = await expectError(await rawMutate(operator, 'POST', '/api/v1/parameters', '{', 'application/json'), 422, 'VALIDATION_FAILED');
  expect(
    malformed.issues.map((issue) => ({
      field: issue.field,
      code: issue.code,
    })),
  ).toEqual([{ field: '', code: 'BODY_MALFORMED' }]);

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
