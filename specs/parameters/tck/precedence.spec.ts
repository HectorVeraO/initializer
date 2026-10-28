import { expect, test } from '@playwright/test';
import { bootstrapOperator, mutate } from '../../tck-support/actors';
import { seedTenant } from '../../tck-support/actors/tenants';
import type { Actor } from '../../tck-support/actors';
import type { SeededTenant } from '../../tck-support/actors/tenants';
import { defineParameter, expectView, overridePath, paramKey, readParameter } from './support';

test.describe('parameters precedence', () => {
  test.describe.configure({ mode: 'serial' });

  const key = paramKey('retry.max_attempts');
  let operator: Actor;
  let tenantA: SeededTenant;
  let tenantB: SeededTenant;

  test('P1 a tenant override wins for its tenant only', { tag: '@parameters' }, async ({ playwright }) => {
    operator = await bootstrapOperator(playwright);
    const created = await defineParameter(operator, {
      key,
      valueType: 'number',
      defaultValue: 3,
      minValue: 1,
      maxValue: 10,
      isSystemRequired: true,
    });
    expect(created.source).toBe('global');
    tenantA = await seedTenant(playwright, operator, { slugBase: 'p1-a' });
    tenantB = await seedTenant(playwright, operator, { slugBase: 'p1-b' });

    const set = await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 5 }), 200, key);
    expect(set.effectiveValue).toBe(5);
    expect(set.source).toBe('tenant');

    const readA = await readParameter(tenantA.admin, key);
    expect([readA.effectiveValue, readA.source]).toEqual([5, 'tenant']);
    const readB = await readParameter(tenantB.admin, key);
    expect([readB.effectiveValue, readB.source]).toEqual([3, 'global']);
  });

  test('P2 clearing an override restores the global value and is idempotent', { tag: '@parameters' }, async () => {
    for (const attempt of [1, 2]) {
      const cleared = await expectView(await mutate(tenantA.admin, 'DELETE', overridePath(key)), 200, key);
      expect([attempt, cleared.effectiveValue, cleared.source]).toEqual([attempt, 3, 'global']);
    }
  });

  test('P3 a tenant cannot clear another tenant override', { tag: '@parameters' }, async () => {
    await expectView(await mutate(tenantA.admin, 'PUT', overridePath(key), { value: 5 }), 200, key);

    const clearedB = await expectView(await mutate(tenantB.admin, 'DELETE', overridePath(key)), 200, key);
    expect([clearedB.effectiveValue, clearedB.source]).toEqual([3, 'global']);

    const readA = await readParameter(tenantA.admin, key);
    expect([readA.effectiveValue, readA.source]).toEqual([5, 'tenant']);
    const readB = await readParameter(tenantB.admin, key);
    expect([readB.effectiveValue, readB.source]).toEqual([3, 'global']);
  });
});
