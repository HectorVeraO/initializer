import { test, expect } from '@playwright/test';

test('alpha reads its tckEnv', { tag: '@alpha' }, async () => {
  expect(process.env.TCK_FIXTURE_VALUE).toBe(process.env.TCK_FIXTURE_EXPECTED ?? 'from-manifest');
});
