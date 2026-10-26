import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { collectTags, loadManifests, narrowTags, TckError } from './tck-select.mjs';
import { buildExports } from './tck-env.mjs';
import { planRun } from './run-tck.mjs';
import { checkTests } from './check-tags.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const fixtures = join(here, 'fixtures');

function manifestsOf(...names) {
  const all = loadManifests(join(fixtures, 'manifests'));
  return all.filter((manifest) => names.includes(manifest.spec));
}

function grepOf(pass) {
  const index = pass.args.indexOf('--grep');
  return new RegExp(pass.args[index + 1]);
}

function runNode(script, args, options = {}) {
  const result = spawnSync(process.execPath, [join(here, script), ...args], { encoding: 'utf8', ...options });
  return result;
}

test('--tags @core matches a @core title and not a @core-auth title', () => {
  const [pass] = planRun({ specsDir: join(fixtures, 'manifests'), tags: '@core' });
  const grep = grepOf(pass);
  assert.match('login works @core', grep);
  assert.doesNotMatch('login works @core-auth', grep);
});

test('the default plan keeps @core-auth selectable next to @core', () => {
  const passes = planRun({ specsDir: join(fixtures, 'manifests'), tags: '@core,@core-auth' });
  const grep = grepOf(passes[0]);
  assert.match('a @core', grep);
  assert.match('a @core-auth', grep);
  assert.doesNotMatch('a @core-authx', grep);
});

function workersOf(pass) {
  const joined = pass.args.join(' ');
  const match = /--workers[= ](\S+)/.exec(joined);
  const workers = match ? match[1] : undefined;
  return workers;
}

test('installed @feature-flags adds a serial second pass and is inverted out of the first', () => {
  const passes = planRun({ specsDir: join(fixtures, 'manifests') });
  assert.equal(passes.length, 2);
  const [main, flags] = passes;
  assert.doesNotMatch('x @feature-flags', grepOf(main));
  const invert = new RegExp(main.args[main.args.indexOf('--grep-invert') + 1]);
  assert.match('x @feature-flags', invert);
  assert.doesNotMatch('x @core', invert);
  assert.equal(workersOf(flags), '1');
  assert.equal(workersOf(main), undefined);
  assert.match('x @feature-flags', grepOf(flags));
});

test('no @feature-flags means a single pass', () => {
  const passes = planRun({ specsDir: join(fixtures, 'manifests'), tags: '@core,@otel' });
  assert.equal(passes.length, 1);
});

test('an uninstalled --tags value is rejected with exit code 2', () => {
  assert.throws(
    () => narrowTags(collectTags(manifestsOf('core')), ['@parameters']),
    (error) => error instanceof TckError && error.exitCode === 2 && /@parameters/.test(error.message),
  );
});

test('a malformed manifest tag is rejected with exit code 2', () => {
  assert.throws(
    () => collectTags([{ spec: 'bad', tckTags: ['core'] }]),
    (error) => error instanceof TckError && error.exitCode === 2 && /invalid tag "core" in spec bad/.test(error.message),
  );
});

test('an empty --tags list is rejected with exit code 2', () => {
  assert.throws(
    () => narrowTags(collectTags(manifestsOf('core')), []),
    (error) => error instanceof TckError && error.exitCode === 2 && /at least one tag/.test(error.message),
  );
});

test('run-tck --dry-run prints the planned commands', () => {
  const result = runNode('run-tck.mjs', ['--dry-run', '--specs-dir', join(fixtures, 'manifests')]);
  assert.equal(result.status, 0);
  const lines = result.stdout.trim().split('\n');
  assert.equal(lines.length, 2);
  assert.match(lines[0], /^playwright test --grep /);
  assert.match(lines[1], /--workers 1$/);
});

test('run-tck --tags with an uninstalled tag exits 2', () => {
  const result = runNode('run-tck.mjs', ['--dry-run', '--tags', '@nope', '--specs-dir', join(fixtures, 'manifests')]);
  assert.equal(result.status, 2);
  assert.match(result.stderr, /tag not installed: @nope/);
});

test('run-tck with no manifest exits non-zero with a clear message', () => {
  const result = runNode('run-tck.mjs', ['--dry-run', '--specs-dir', join(fixtures, 'tag-roots', 'ok', 'alpha', 'tck')]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /no spec\.manifest\.json found/);
});

test('tck-env prints sorted exports with escaped single quotes', () => {
  const lines = buildExports(manifestsOf('core', 'core-auth', 'quote'));
  assert.deepEqual(lines, ["export ALPHA_KEY='a'", "export MSG='it'\\''s'", "export ZED_KEY='z'"]);
});

test('tck-env fails on a conflicting key and accepts an identical repeat', () => {
  assert.throws(
    () => buildExports(manifestsOf('conflict-a', 'conflict-b')),
    (error) => error instanceof TckError && error.exitCode === 1 && /SHARED/.test(error.message),
  );
  const same = [
    { spec: 'a', tckEnv: { K: 'v' } },
    { spec: 'b', tckEnv: { K: 'v' } },
  ];
  assert.deepEqual(buildExports(same), ["export K='v'"]);
});

test('tck-env rejects a key that is not a shell identifier', () => {
  assert.throws(() => buildExports([{ spec: 'a', tckEnv: { 'X; rm': '1' } }]), TckError);
});

test('tck-env rejects a non-string value', () => {
  assert.throws(
    () => buildExports([{ spec: 'a', tckEnv: { PORT: 8080 } }]),
    (error) => error instanceof TckError && error.exitCode === 1 && /PORT must be a string/.test(error.message),
  );
});

test('checkTests reports untagged and foreign tags', () => {
  const manifests = [
    { spec: 'alpha', tckTags: ['@alpha'] },
    { spec: 'beta', tckTags: ['@beta'] },
  ];
  const problems = checkTests(
    [
      { file: 'alpha/tck/a.spec.ts', line: 3, title: 'fine', tags: ['@alpha'] },
      { file: 'alpha/tck/a.spec.ts', line: 5, title: 'bare', tags: [] },
      { file: 'alpha/tck/a.spec.ts', line: 7, title: 'mixed', tags: ['@alpha', '@beta'] },
    ],
    manifests,
  );
  assert.equal(problems.length, 2);
  assert.match(problems[0], /^TCK_TEST_UNTAGGED: alpha\/tck\/a\.spec\.ts:5 "bare" lacks a tag of specs\/alpha$/);
  assert.match(problems[1], /^TCK_TAG_FOREIGN: .*"mixed" carries @beta of specs\/beta$/);
});

test('checkTests reports a test under a folder without a manifest', () => {
  const problems = checkTests(
    [{ file: 'orphan/tck/o.spec.ts', line: 2, title: 'lost', tags: ['@alpha'] }],
    [{ spec: 'alpha', tckTags: ['@alpha'] }],
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /^TCK_SPEC_MANIFEST_MISSING: orphan\/tck\/o\.spec\.ts:2 "lost" sits under specs\/orphan\//);
});

test('loadManifests rejects a non-array tckTags with exit code 2', () => {
  const root = mkdtempSync(join(tmpdir(), 'tck-manifest-'));
  try {
    mkdirSync(join(root, 'bad'));
    writeFileSync(join(root, 'bad', 'spec.manifest.json'), JSON.stringify({ spec: 'bad', tckTags: '@bad' }));
    assert.throws(
      () => loadManifests(root),
      (error) => error instanceof TckError && error.exitCode === 2 && /tckTags must be an array/.test(error.message),
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

function checkRoot(name) {
  const root = join(fixtures, 'tag-roots', name);
  const result = runNode('check-tags.mjs', ['--root', root]);
  return result;
}

test('check-tags passes on a consistent fixture root', () => {
  const result = checkRoot('ok');
  assert.equal(result.status, 0, result.stderr);
});

test('check-tags fails TCK_TEST_UNTAGGED on an untagged fixture test', () => {
  const result = checkRoot('untagged');
  assert.equal(result.status, 1);
  assert.match(result.stderr, /TCK_TEST_UNTAGGED: alpha\/tck\/a\.spec\.ts:\d+ "alpha untagged"/);
});

test('check-tags fails TCK_TAG_FOREIGN on a foreign tag', () => {
  const result = checkRoot('foreign');
  assert.equal(result.status, 1);
  assert.match(result.stderr, /TCK_TAG_FOREIGN: alpha\/tck\/a\.spec\.ts:\d+ "alpha foreign" carries @beta/);
});
