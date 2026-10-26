import * as dotenv from 'dotenv';
import * as path from 'path';
import type { BrowserContextOptions } from '@playwright/test';

dotenv.config({ path: path.resolve(__dirname, '..', '.env') });

const E2E_ROOT = path.resolve(__dirname, '..');

// Single source for the viewport AND the video canvas: every reader below goes
// through readViewport(), so the two can never disagree.
const DEFAULT_VIEWPORT: Viewport = { width: 1600, height: 900 };

// Values of INIT_TEST_GREP_INVERT that switch the @manual exclusion off.
// `none` exists because PowerShell deletes a variable assigned ''.
const GREP_INVERT_DISABLED = ['', 'none'];

type VideoMode = 'off' | 'on' | 'retain-on-failure' | 'on-first-retry';
type ScreenshotMode = 'off' | 'on' | 'only-on-failure';
type TraceMode = 'off' | 'on' | 'retain-on-failure' | 'on-first-retry';
type Viewport = { width: number; height: number };

type EnvShape = {
  web: { baseUrl: string; loginPath: string };
  auth: { email: string; password: string };
  timeouts: { default: number; navigation: number; action: number };
  reporting: { htmlDir: string; resultsDir: string };
  artifacts: {
    video: VideoMode;
    screenshot: ScreenshotMode;
    trace: TraceMode;
    viewport: Viewport;
  };
  workers: number;
};

export type EnvCheck = { ok: true } | { ok: false; missing: string[] };

// TODO(hvera): list the env keys your REAL tests require (e.g. credentials).
// Left empty so the base suite stays green before any real tests exist.
const REQUIRED_KEYS: readonly string[] = [];

function readString(key: string, fallback = ''): string {
  const raw = process.env[key];
  return raw === undefined || raw === '' ? fallback : raw;
}

function readInt(key: string, fallback: number): number {
  const raw = process.env[key];
  if (raw === undefined || raw === '') {
    return fallback;
  }
  const parsed = Number.parseInt(raw, 10);
  return Number.isNaN(parsed) ? fallback : parsed;
}

/**
 * Browser viewport, which is also the video size. Reads
 * INIT_VIEWPORT_WIDTH / INIT_VIEWPORT_HEIGHT.
 */
export function readViewport(): Viewport {
  const viewport = {
    width: readInt('INIT_VIEWPORT_WIDTH', DEFAULT_VIEWPORT.width),
    height: readInt('INIT_VIEWPORT_HEIGHT', DEFAULT_VIEWPORT.height),
  };
  return viewport;
}

/**
 * The config-level grepInvert. Unset: exclude @manual. Empty or `none`: no
 * exclusion. Anything else: used as the regex. The CLI cannot lift a config
 * grepInvert, which is why this is an env var and not a flag.
 */
export function readGrepInvert(): RegExp | undefined {
  const raw = process.env['INIT_TEST_GREP_INVERT'];
  if (raw === undefined) {
    return /@manual/;
  }
  if (GREP_INVERT_DISABLED.includes(raw.trim())) {
    return undefined;
  }
  return new RegExp(raw);
}

export function loadEnv(): EnvShape {
  return {
    web: {
      baseUrl: readString('INIT_WEB_BASE_URL', 'http://localhost:10120'),
      loginPath: readString('INIT_WEB_LOGIN_PATH', ''),
    },
    auth: {
      email: readString('INIT_USER_EMAIL'),
      password: readString('INIT_USER_PASSWORD'),
    },
    timeouts: {
      default: readInt('INIT_DEFAULT_TIMEOUT', 30000),
      navigation: readInt('INIT_NAV_TIMEOUT', 30000),
      action: readInt('INIT_ACTION_TIMEOUT', 10000),
    },
    reporting: {
      htmlDir: readString(
        'PW_HTML_REPORT_DIR',
        path.join(E2E_ROOT, 'playwright-report'),
      ),
      resultsDir: readString(
        'PW_RESULTS_DIR',
        path.join(E2E_ROOT, 'test-results'),
      ),
    },
    // Code defaults are the CI values; .example.env sets the local ones.
    artifacts: {
      video: readString('PW_VIDEO', 'retain-on-failure') as VideoMode,
      screenshot: readString(
        'PW_SCREENSHOT',
        'only-on-failure',
      ) as ScreenshotMode,
      trace: readString('PW_TRACE', 'on-first-retry') as TraceMode,
      viewport: readViewport(),
    },
    workers: readInt('PLAYWRIGHT_WORKERS', 2),
  };
}

/**
 * Viewport and recording options for a context created by hand with
 * `browser.newContext()`, which inherits neither `use.viewport` nor `use.video`.
 * `recordVideo` is present only when PW_VIDEO=on: the other modes are applied by
 * Playwright to fixture-managed contexts only.
 *
 * @param videoDir Where the raw .webm lands. Pass `testInfo.outputPath()` to keep
 *   it inside the test's own folder.
 */
export function manualContextOptions(
  videoDir: string,
): Pick<BrowserContextOptions, 'viewport' | 'recordVideo'> {
  const viewport = readViewport();
  if (loadEnv().artifacts.video !== 'on') {
    return { viewport };
  }
  const options = { viewport, recordVideo: { dir: videoDir, size: viewport } };
  return options;
}

/**
 * Reports which of `keys` are unset or empty. Every assert*Env guard is built on
 * it, so a spec that needs optional configuration skips cleanly instead of failing.
 */
export function checkEnvKeys(keys: readonly string[]): EnvCheck {
  const missing = keys.filter((key) => {
    const value = process.env[key];
    return value === undefined || value === '';
  });
  return missing.length > 0 ? { ok: false, missing } : { ok: true };
}

export function assertRequiredEnv(): EnvCheck {
  return checkEnvKeys(REQUIRED_KEYS);
}

/**
 * Guard for specs that log in. Call it at the top of the spec and skip when it
 * fails:
 *
 *   const creds = assertCredentialsEnv();
 *   test.skip(!creds.ok, creds.ok ? '' : `missing env: ${creds.missing.join(', ')}`);
 *
 * Add one assert<Area>Env per optional configuration group, next to this one,
 * before reading a bare process.env value in a spec.
 */
export function assertCredentialsEnv(): EnvCheck {
  return checkEnvKeys(['INIT_USER_EMAIL', 'INIT_USER_PASSWORD']);
}

export function buildBaseUrl(env: EnvShape): string {
  return new URL('/', env.web.baseUrl).toString();
}

export function buildWebLoginUrl(env: EnvShape): string {
  return new URL(env.web.loginPath || '/', env.web.baseUrl).toString();
}
