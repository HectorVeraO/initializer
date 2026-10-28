import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { loadManifests } from './tck-select.mjs';

const SPECS_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const PLACEHOLDER_BASE_URL = 'http://localhost:10100';

/**
 * Flattens a Playwright JSON list report (which drops the leading @ of tags) into `{ file, line, title, tags }`.
 * @param {{ suites?: object[] }} report
 */
export function flattenReport(report) {
  const tests = [];
  const visit = (suite) => {
    for (const spec of suite.specs ?? []) {
      tests.push({ file: spec.file, line: spec.line, title: spec.title, tags: (spec.tags ?? []).map((tag) => (tag.startsWith('@') ? tag : `@${tag}`)) });
    }
    for (const child of suite.suites ?? []) {
      visit(child);
    }
  };
  for (const suite of report.suites ?? []) {
    visit(suite);
  }
  return tests;
}

/**
 * Checks every listed test against the manifests' `tckTags` and `cooperatesWith`.
 * Returns one message per violation; empty means the tags are consistent.
 * @param {{ file: string, line: number, title: string, tags: string[] }[]} tests files relative to the specs root
 * @param {{ spec: string, tckTags: string[], cooperatesWith?: string[] }[]} manifests
 * @returns {string[]}
 */
export function checkTests(tests, manifests) {
  const problems = [];
  for (const test of tests) {
    const folder = test.file.split(/[\\/]/)[0];
    const owner = manifests.find((manifest) => manifest.spec === folder);
    const where = `${test.file}:${test.line} "${test.title}"`;
    if (owner === undefined) {
      problems.push(`TCK_SPEC_MANIFEST_MISSING: ${where} sits under specs/${folder}/ without a spec.manifest.json`);
      continue;
    }
    if (!test.tags.some((tag) => owner.tckTags.includes(tag))) {
      problems.push(`TCK_TEST_UNTAGGED: ${where} lacks a tag of specs/${owner.spec}`);
    }
    const cooperating = owner.cooperatesWith ?? [];
    for (const tag of test.tags) {
      if (owner.tckTags.includes(tag) || cooperating.includes(tag)) {
        continue;
      }
      const other = manifests.find((manifest) => manifest.spec !== owner.spec && manifest.tckTags.includes(tag));
      if (other === undefined) {
        problems.push(`TCK_TAG_FOREIGN: ${where} carries ${tag}, which no installed spec declares`);
      } else {
        problems.push(`TCK_TAG_FOREIGN: ${where} carries ${tag} of specs/${other.spec}`);
      }
    }
  }
  return problems;
}

function listTests(root) {
  const cli = createRequire(import.meta.url).resolve('@playwright/test/cli');
  const env = { ...process.env };
  env.TCK_BASE_URL = env.TCK_BASE_URL || PLACEHOLDER_BASE_URL;
  const result = spawnSync(process.execPath, [cli, 'test', '--list', '--reporter=json'], {
    cwd: root,
    env,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  });
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch {
    throw new Error(`playwright --list exited ${result.status} without a JSON report\n${result.stderr || result.stdout || ''}`);
  }
  // An empty tree (no suite installed yet) is consistent: Playwright exits 1 with "No tests found".
  const reportErrors = (report.errors ?? [])
    .map((entry) => String(entry.message))
    .filter((message) => !message.includes('No tests found'));
  if (reportErrors.length > 0 || (result.status !== 0 && (report.errors ?? []).length === 0)) {
    throw new Error(`playwright --list exited ${result.status}\n${reportErrors.join('\n') || result.stderr}`);
  }
  return flattenReport(report);
}

function main() {
  const { values } = parseArgs({ options: { root: { type: 'string' } }, strict: true });
  const root = values.root ? resolve(values.root) : SPECS_ROOT;
  let problems;
  try {
    problems = checkTests(listTests(root), loadManifests(root));
  } catch (error) {
    process.stderr.write(`check-tags: ${error.message}\n`);
    process.exit(2);
  }
  if (problems.length > 0) {
    process.stderr.write(`${problems.join('\n')}\n`);
    process.exit(1);
  }
  process.stdout.write('check-tags: ok\n');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
