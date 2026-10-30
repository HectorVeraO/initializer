import { test, expect } from '@playwright/test';

test('the instance origin and flags file reach the suite', { tag: '@core' }, async () => {
  expect(process.env.TCK_BASE_URL).toBe(process.env.TCK_FIXTURE_EXPECTED_URL);
  expect(process.env.APP_PUBLIC_BASE_URL).toBe(process.env.TCK_FIXTURE_EXPECTED_URL);
  expect(process.env.APP_FLAGS_FILE ?? '').toBe(process.env.TCK_FIXTURE_EXPECTED_FLAGS ?? '');
});
