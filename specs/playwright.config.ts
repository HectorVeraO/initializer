import { defineConfig } from '@playwright/test';

const baseURL = process.env.TCK_BASE_URL;
if (!baseURL) {
  throw new Error('TCK_BASE_URL is required: set it to the GATBASE_PUBLIC_BASE_URL of the instance under test');
}

// Tag selection lives in scripts/run-tck.mjs; this config sets no grep.
export default defineConfig({
  testDir: '.',
  testMatch: '*/tck/**/*.spec.ts',
  // scripts/** keeps the runner's fixture specs (scripts/fixtures/**/tck/*.spec.ts) out of a real run.
  testIgnore: ['node_modules/**', 'tck-support/**', 'scripts/**'],
  forbidOnly: !!process.env.CI,
  retries: 0,
  fullyParallel: true,
  reporter: [['list'], ['junit', { outputFile: 'test-results/junit.xml' }]],
  use: { baseURL },
});
