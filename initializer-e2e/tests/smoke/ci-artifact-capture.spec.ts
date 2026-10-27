import * as dotenv from 'dotenv';
import { spawnSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { test, expect } from '../../src/fixtures/test';
import { loadEnv } from '../../src/env';

const ARTIFACT_KEYS = ['PW_VIDEO', 'PW_SCREENSHOT', 'PW_TRACE'] as const;
const REPO_ROOT = path.resolve(__dirname, '..', '..', '..');
const TEMPLATE = path.join(REPO_ROOT, 'initializer-e2e', '.example.env');
const WORKFLOW = path.join(REPO_ROOT, '.github', 'workflows', 'ci.yml');

/** The workflow line that writes initializer-e2e/.env from the template. */
function findEnvCopyCommand(): string {
  const workflow = fs.readFileSync(WORKFLOW, 'utf8');
  const lines = workflow
    .split('\n')
    .map((line) => line.trim())
    .filter(
      (line) =>
        line.includes('initializer-e2e/.example.env') &&
        line.includes('initializer-e2e/.env'),
    );
  expect(lines, 'one CI step writes initializer-e2e/.env').toHaveLength(1);
  return lines[0];
}

/** Runs the CI command in a scratch tree and returns the .env it writes. */
function runCiEnvCopy(command: string): Record<string, string> {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'ci-env-'));
  try {
    fs.mkdirSync(path.join(scratch, 'initializer-e2e'));
    fs.copyFileSync(
      TEMPLATE,
      path.join(scratch, 'initializer-e2e', '.example.env'),
    );
    const result = spawnSync('bash', ['-c', `set -euo pipefail; ${command}`], {
      cwd: scratch,
      encoding: 'utf8',
    });
    expect(result.status, result.stderr).toBe(0);
    const written = fs.readFileSync(
      path.join(scratch, 'initializer-e2e', '.env'),
    );
    return dotenv.parse(written);
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true });
  }
}

test.describe('CI artifact capture', () => {
  test('@smoke @issue-98 the .env the e2e-gatbase job writes keeps the CI failure-only defaults', () => {
    const ciDotenv = runCiEnvCopy(findEnvCopyCommand());
    const saved = Object.fromEntries(
      ARTIFACT_KEYS.map((key) => [key, process.env[key]]),
    );
    try {
      for (const key of ARTIFACT_KEYS) {
        // The CI job sets none of these itself, so only the written .env counts.
        delete process.env[key];
        const value = ciDotenv[key];
        if (value !== undefined) {
          process.env[key] = value;
        }
      }
      const { artifacts } = loadEnv();

      expect(artifacts.video).toBe('retain-on-failure');
      expect(artifacts.screenshot).toBe('only-on-failure');
      expect(artifacts.trace).toBe('on-first-retry');
    } finally {
      for (const key of ARTIFACT_KEYS) {
        const value = saved[key];
        if (value === undefined) {
          delete process.env[key];
        } else {
          process.env[key] = value;
        }
      }
    }
  });
});
