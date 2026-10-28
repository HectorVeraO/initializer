import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectError, mutate } from '../../tck-support/actors';
import { defineParameter, paramKey, readParameter } from './support';

test('P4 a system-required parameter cannot be deleted', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const key = paramKey('protect.retry_attempts');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 3,
    minValue: 1,
    maxValue: 10,
    isSystemRequired: true,
  });

  await expectError(await mutate(operator, 'DELETE', `/api/v1/parameters/${key}`), 409, 'SYSTEM_PARAMETER_PROTECTED');

  const stillListed = await readParameter(operator, key);
  expect(stillListed.isSystemRequired).toBe(true);
});
