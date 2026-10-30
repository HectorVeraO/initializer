import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { TckError } from './tck-select.mjs';
import { planComponentRun } from './run-tck.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const project = join(here, 'fixtures', 'multi');
const specsDir = join(project, 'specs');
const flagsFile = 'specs/feature-flags/tck/fixtures/flags.yaml';

function runNode(args, env = process.env) {
  const result = spawnSync(process.execPath, [join(here, 'run-tck.mjs'), ...args, '--specs-dir', specsDir], { encoding: 'utf8', env });
  return result;
}

function regexOf(pass, flag) {
  const index = pass.args.indexOf(flag);
  const pattern = index === -1 ? undefined : new RegExp(pass.args[index + 1]);
  return pattern;
}

/** Runs `check` against a copy of the fixture project whose lock `edit` changed. */
function withLock(edit, check) {
  const root = mkdtempSync(join(tmpdir(), 'tck-component-'));
  try {
    cpSync(specsDir, join(root, 'specs'), { recursive: true });
    const lock = JSON.parse(readFileSync(join(project, 'initializer.lock.json'), 'utf8'));
    edit(lock);
    writeFileSync(join(root, 'initializer.lock.json'), JSON.stringify(lock));
    check(join(root, 'specs'), root);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

function messageOf(options) {
  try {
    planComponentRun({ env: {}, ...options });
  } catch (error) {
    assert.ok(error instanceof TckError, `want a TckError, got ${error}`);
    assert.equal(error.exitCode, 2);
    return error.message;
  }
  return assert.fail('want a TckError');
}

test('--component selects the tags and the cooperation invert of that backend', () => {
  const first = planComponentRun({ specsDir, component: 'acme-srv-first', env: {} });
  const second = planComponentRun({ specsDir, component: 'acme-srv-second', env: {} });
  assert.equal(first.passes.length, 1);
  assert.match('a @identity @parameters', regexOf(first.passes[0], '--grep'));
  assert.doesNotMatch('a @feature-flags', regexOf(first.passes[0], '--grep'));
  assert.doesNotMatch('a @identity', regexOf(first.passes[0], '--grep-invert'));
  assert.equal(second.passes.length, 2);
  assert.doesNotMatch('a @identity', regexOf(second.passes[0], '--grep'));
  assert.match('a @parameters @identity', regexOf(second.passes[0], '--grep-invert'));
  assert.match('a @feature-flags @identity', regexOf(second.passes[1], '--grep-invert'));
  // api-keys cooperates with @tenancy, but no backend installs api-keys.
  for (const pass of second.passes) {
    assert.doesNotMatch('a @tenancy', regexOf(pass, '--grep-invert'));
  }
});

test('--component keeps --tags semantics inside the backend tags', () => {
  const narrowed = planComponentRun({ specsDir, component: 'acme-srv-first', tags: '@core', env: {} });
  assert.equal(narrowed.passes.length, 1);
  assert.doesNotMatch('a @identity', regexOf(narrowed.passes[0], '--grep'));
  assert.match(messageOf({ specsDir, component: 'acme-srv-first', tags: '@feature-flags' }), /tag not installed: @feature-flags/);
});

test('--component takes the lock env, sets both origin keys and lets exported variables win otherwise', () => {
  const first = planComponentRun({ specsDir, component: 'acme-srv-first', env: {} });
  assert.deepEqual([first.env.TCK_BASE_URL, first.env.APP_PUBLIC_BASE_URL, first.env.APP_FLAGS_FILE], ['http://localhost:10100', 'http://localhost:10100', undefined]);
  const second = planComponentRun({ specsDir, component: 'acme-srv-second', env: {} });
  assert.deepEqual([second.env.TCK_BASE_URL, second.env.APP_PUBLIC_BASE_URL, second.env.APP_FLAGS_FILE], ['http://localhost:10120', 'http://localhost:10120', flagsFile]);
  const exported = { APP_FLAGS_FILE: 'from-caller', TCK_BASE_URL: 'http://wrong', APP_PUBLIC_BASE_URL: 'http://wrong', KEEP: 'yes' };
  const winning = planComponentRun({ specsDir, component: 'acme-srv-second', env: exported });
  assert.deepEqual([winning.env.APP_FLAGS_FILE, winning.env.TCK_BASE_URL, winning.env.APP_PUBLIC_BASE_URL, winning.env.KEEP], ['from-caller', 'http://localhost:10120', 'http://localhost:10120', 'yes']);
});

test('--component exits 2 without a lock', () => {
  const result = spawnSync(process.execPath, [join(here, 'run-tck.mjs'), '--dry-run', '--component', 'acme-srv-first', '--specs-dir', join(here, 'fixtures', 'manifests')], { encoding: 'utf8' });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /--component needs .*lock\.json/);
});

test('--component exits 2 on a single lock, an unknown name and a frontend name', () => {
  withLock((lock) => {
    lock.components = [lock.components[0], lock.components[2]];
    lock.components[1].backend = lock.components[0].name;
  }, (specs) => {
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /needs a project with two or more backends; run without it/);
  });
  const unknown = runNode(['--dry-run', '--component', 'acme-srv-third']);
  assert.equal(unknown.status, 2);
  assert.match(unknown.stderr, /unknown component "acme-srv-third"; backends: acme-srv-first, acme-srv-second/);
  const frontend = runNode(['--dry-run', '--component', 'acme-web']);
  assert.equal(frontend.status, 2);
  assert.match(frontend.stderr, /acme-web is a frontend component; backends: acme-srv-first, acme-srv-second/);
});

test('--component exits 2 when the lock entry has no origin, no tck, a bad tag or no JSON', () => {
  withLock((lock) => delete lock.components[0].origin, (specs) => {
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /component acme-srv-first has no origin in the lock/);
  });
  withLock((lock) => delete lock.components[0].tck, (specs) => {
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /component acme-srv-first has no tck in the lock/);
  });
  withLock((lock) => (lock.components[0].tck.tags = ['@ok', 'not a tag']), (specs, root) => {
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /invalid tag "not a tag"/);
    writeFileSync(join(root, 'initializer.lock.json'), '{ not json');
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /not valid JSON/);
    writeFileSync(join(root, 'initializer.lock.json'), '{"schema":2}');
    assert.match(messageOf({ specsDir: specs, component: 'acme-srv-first' }), /no components list/);
  });
});

test('a plain run ignores the lock and --component --dry-run prints the passes of that backend', () => {
  const plain = runNode(['--dry-run']);
  assert.equal(plain.status, 0, plain.stderr);
  // @api-keys and @tenancy are installed in the specs tree but in no lock backend: a plain run still selects them.
  assert.match(plain.stdout.split('\n')[0], /@api\\-keys.*@tenancy/);
  assert.match(plain.stdout, /@feature\\-flags/);
  const second = runNode(['--dry-run', '--component', 'acme-srv-second']);
  assert.equal(second.status, 0, second.stderr);
  const lines = second.stdout.trim().split('\n');
  assert.equal(lines.length, 2);
  assert.doesNotMatch(lines[0], /--grep \S*@tenancy/);
  assert.match(lines[1], /--workers 1$/);
});

test('--component runs Playwright with the origin of the lock entry and its env', () => {
  const base = { ...process.env, TCK_BASE_URL: 'http://wrong', APP_PUBLIC_BASE_URL: 'http://wrong' };
  delete base.APP_FLAGS_FILE;
  const second = runNode(['--component', 'acme-srv-second', '--tags', '@core'], { ...base, TCK_FIXTURE_EXPECTED_URL: 'http://localhost:10120', TCK_FIXTURE_EXPECTED_FLAGS: flagsFile });
  assert.equal(second.status, 0, `${second.stdout}${second.stderr}`);
  const first = runNode(['--component', 'acme-srv-first', '--tags', '@core'], { ...base, TCK_FIXTURE_EXPECTED_URL: 'http://localhost:10100', TCK_FIXTURE_EXPECTED_FLAGS: '' });
  assert.equal(first.status, 0, `${first.stdout}${first.stderr}`);
  const mismatch = runNode(['--component', 'acme-srv-second', '--tags', '@core'], { ...base, TCK_FIXTURE_EXPECTED_URL: 'http://localhost:10100', TCK_FIXTURE_EXPECTED_FLAGS: '' });
  assert.equal(mismatch.status, 1);
  // Playwright reports the expected origin and the one the suite received.
  assert.match(`${mismatch.stdout}${mismatch.stderr}`, /http:\/\/localhost:10100[\s\S]*http:\/\/localhost:10120/);
});
