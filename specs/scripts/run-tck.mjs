import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { mergeTckEnv } from './tck-env.mjs';
import { collectInvertTags, collectTags, loadManifests, narrowTags, parseTagList, planPasses, TckError } from './tck-select.mjs';

const SPECS_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');

function shellWord(word) {
  const isPlain = /^[\w@.,=/:-]+$/.test(word);
  const quoted = isPlain ? word : `'${word.replaceAll("'", "'\\''")}'`;
  return quoted;
}

/**
 * Plans the Playwright passes for the specs installed under `specsDir`.
 * @param {{ specsDir: string, tags?: string }} options
 */
export function planRun({ specsDir, tags }) {
  const manifests = loadManifests(specsDir);
  if (manifests.length === 0) {
    throw new TckError(`no spec.manifest.json found under ${specsDir}/*/`, 1);
  }
  const installed = collectTags(manifests);
  const selected = narrowTags(installed, parseTagList(tags));
  const invert = collectInvertTags(manifests, installed);
  return planPasses(selected, invert);
}

/** Reads `<specsDir>/../initializer.lock.json`; a missing or malformed lock is a TckError (exit 2). */
function readProjectLock(specsDir) {
  const path = join(specsDir, '..', 'initializer.lock.json');
  if (!existsSync(path)) {
    throw new TckError(`--component needs ${path}; run without it in a project that has no lock`, 2);
  }
  let lock;
  try {
    lock = JSON.parse(readFileSync(path, 'utf8'));
  } catch (error) {
    throw new TckError(`${path} is not valid JSON: ${error.message}`, 2);
  }
  if (lock === null || typeof lock !== 'object' || !Array.isArray(lock.components)) {
    throw new TckError(`${path} has no components list`, 2);
  }
  return lock;
}

/** True when some stack has two or more components. */
function isMultiLock(lock) {
  const counts = new Map();
  for (const component of lock.components) {
    counts.set(component.stack, (counts.get(component.stack) ?? 0) + 1);
  }
  const multi = [...counts.values()].some((count) => count > 1);
  return multi;
}

/**
 * Plans the Playwright passes and the environment for one backend component of
 * a multi project, from the TCK selection its lock entry records. A scenario
 * runs only when every tag it carries is one the component installs. The two
 * origin keys come from the lock and win over exported values; every other
 * exported variable wins over the entry's `tck.env`.
 * @param {{ specsDir: string, component: string, tags?: string, env?: NodeJS.ProcessEnv }} options
 * @returns {{ passes: { name: string, args: string[] }[], env: NodeJS.ProcessEnv }}
 */
export function planComponentRun({ specsDir, component, tags, env = process.env }) {
  const lock = readProjectLock(specsDir);
  if (!isMultiLock(lock)) {
    throw new TckError('--component needs a project with two or more backends; run without it', 2);
  }
  // A frontend entry names its backend; a backend entry does not.
  const backends = lock.components.filter((entry) => entry.backend === undefined);
  const backendNames = backends.map((entry) => entry.name).join(', ');
  const entry = lock.components.find((candidate) => candidate.name === component);
  if (entry === undefined) {
    throw new TckError(`unknown component ${JSON.stringify(component)}; backends: ${backendNames}`, 2);
  }
  if (entry.backend !== undefined) {
    throw new TckError(`${component} is a frontend component; backends: ${backendNames}`, 2);
  }
  const tck = entry.tck;
  if (tck === undefined || !Array.isArray(tck.tags)) {
    throw new TckError(`component ${component} has no tck in the lock`, 2);
  }
  if (typeof entry.origin !== 'string' || entry.origin === '') {
    throw new TckError(`component ${component} has no origin in the lock`, 2);
  }
  const installed = collectTags([{ spec: component, tckTags: tck.tags }]);
  const allManifests = loadManifests(specsDir);
  const componentManifests = allManifests.filter((manifest) => manifest.tckTags.every((tag) => installed.includes(tag)));
  const selected = narrowTags(installed, parseTagList(tags));
  // The specs folder holds the union of every backend's specs: a scenario
  // co-tagged with a module this backend lacks is left out.
  const absentTags = collectTags(allManifests).filter((tag) => !installed.includes(tag));
  const invert = [...new Set([...collectInvertTags(componentManifests, installed), ...absentTags])].sort();
  const passes = planPasses(selected, invert);
  const origin = entry.origin;
  const componentEnv = { ...(tck.env ?? {}), ...env, TCK_BASE_URL: origin, APP_PUBLIC_BASE_URL: origin };
  return { passes, env: componentEnv };
}

function main() {
  const { values } = parseArgs({
    options: {
      tags: { type: 'string' },
      component: { type: 'string' },
      'dry-run': { type: 'boolean', default: false },
      'specs-dir': { type: 'string' },
    },
    strict: true,
  });
  const specsDir = values['specs-dir'] ? resolve(values['specs-dir']) : SPECS_ROOT;
  const isComponentRun = values.component !== undefined;
  let passes;
  let componentEnv;
  try {
    if (isComponentRun) {
      ({ passes, env: componentEnv } = planComponentRun({ specsDir, component: values.component, tags: values.tags }));
    } else {
      passes = planRun({ specsDir, tags: values.tags });
    }
  } catch (error) {
    if (error instanceof TckError) {
      process.stderr.write(`run-tck: ${error.message}\n`);
      process.exit(error.exitCode);
    }
    throw error;
  }

  if (values['dry-run']) {
    for (const pass of passes) {
      process.stdout.write(`playwright ${pass.args.map(shellWord).join(' ')}\n`);
    }
    return;
  }

  // The suite sees the tckEnv values the instance under test was started with; an exported variable wins.
  const env = isComponentRun ? componentEnv : { ...Object.fromEntries(mergeTckEnv(loadManifests(specsDir))), ...process.env };
  const cli = createRequire(import.meta.url).resolve('@playwright/test/cli');
  let failed = false;
  for (const pass of passes) {
    const result = spawnSync(process.execPath, [cli, ...pass.args], { cwd: specsDir, env, stdio: 'inherit' });
    if (result.status !== 0) {
      failed = true;
    }
  }
  process.exit(failed ? 1 : 0);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
