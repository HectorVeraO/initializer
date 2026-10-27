import { defineConfig, devices } from '@playwright/test';
import * as path from 'path';
import { loadEnv, buildBaseUrl, readGrepInvert, gatbaseStack } from './src/env';

const env = loadEnv();
const stack = gatbaseStack();

/** process.env without undefined values, as webServer.env requires. */
function definedEnv(source: NodeJS.ProcessEnv): Record<string, string> {
  const entries = Object.entries(source).filter(
    (entry): entry is [string, string] => entry[1] !== undefined,
  );
  return Object.fromEntries(entries);
}

export default defineConfig({
  testDir: './tests',
  testMatch: /.*\.spec\.ts$/,

  // Tests are independent; one plan case maps 1:1 to one test.
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  // @manual is excluded from the standard run. A config-level grepInvert cannot be
  // lifted from the CLI (--grep / --grep-invert only narrow further), so the override
  // is the INIT_TEST_GREP_INVERT env var (see readGrepInvert in src/env.ts).
  grepInvert: readGrepInvert(),
  // 1 retry local / 2 in CI: flows against a shared env can depend on network timing.
  retries: process.env.CI ? 2 : 1,
  workers: env.workers,

  timeout: env.timeouts.default,
  expect: { timeout: env.timeouts.action },

  reporter: [
    ['list'],
    ['html', { outputFolder: env.reporting.htmlDir, open: 'never' }],
    ['junit', { outputFile: path.join(env.reporting.resultsDir, 'junit.xml') }],
  ],

  outputDir: env.reporting.resultsDir,

  use: {
    baseURL: buildBaseUrl(env),
    actionTimeout: env.timeouts.action,
    navigationTimeout: env.timeouts.navigation,
    trace: env.artifacts.trace,
    // Viewport and video size come from one source. Without an explicit size
    // Playwright scales the recording down to fit 800x800, which blurs dense text.
    viewport: env.artifacts.viewport,
    video: { mode: env.artifacts.video, size: env.artifacts.viewport },
    screenshot: env.artifacts.screenshot,
    locale: 'es-CL',
    timezoneId: 'America/Santiago',
  },

  // go-srv and ng-web start only when INIT_START_STACK=true; the shared compose
  // stack (postgres, kratos) must already run. Never reuse a server: a go-srv on
  // the dev database must not serve this run.
  webServer: stack.start
    ? [
        {
          name: 'go-srv',
          command: process.env.CI
            ? 'go run ./cmd/server'
            : 'mise exec -- go run ./cmd/server',
          cwd: stack.goSrvDir,
          url: 'http://localhost:10100/health/ready',
          reuseExistingServer: false,
          timeout: 180_000,
          stdout: 'pipe',
          env: {
            ...definedEnv(process.env),
            ...stack.goSrvEnv,
            GATBASE_PUBLIC_BASE_URL: env.web.baseUrl,
            GATBASE_BOOTSTRAP_ADMIN_EMAIL: env.auth.email,
            GATBASE_BOOTSTRAP_ADMIN_PASSWORD: env.auth.password,
            GATBASE_FLAGS_FILE: stack.flagsFile,
          },
        },
        {
          name: 'ng-web',
          command: process.env.CI ? 'npm start' : 'mise exec -- npm start',
          cwd: stack.ngWebDir,
          url: 'http://localhost:10120/',
          reuseExistingServer: false,
          timeout: 180_000,
        },
      ]
    : undefined,

  projects: [
    {
      name: 'chromium',
      // The viewport is repeated AFTER the device spread on purpose: the
      // `Desktop Chrome` descriptor carries its own 1280x720 viewport, and a
      // project `use` overrides the top-level `use` key by key. Without this line
      // the page renders at 1280x720 inside a larger video canvas and the
      // recording shows grey bands on the right and bottom.
      use: { ...devices['Desktop Chrome'], viewport: env.artifacts.viewport },
    },
  ],
});
