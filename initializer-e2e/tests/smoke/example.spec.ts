import { test, expect } from '../../src/fixtures/test';

/**
 * Placeholder smoke — SKIPPED on purpose so the base suite is green before any
 * real tests exist. Replace it with a real happy-path check once you know the
 * app's selectors (use `npm run codegen` to discover them), then un-skip.
 *
 * Tag rules: exactly one primary tag per test (@smoke | @e2e | @crud | @file)
 * plus free annotations (@issue-<N>, @flaky, @manual). @manual is excluded from
 * the default run.
 */
test.describe('Smoke: base suite (placeholder)', () => {
  test.skip('@smoke TODO(hvera): replace with a real app-shell smoke', async ({
    examplePage,
  }) => {
    await examplePage.goto();
    await expect(examplePage.root).toBeVisible();
  });
});
