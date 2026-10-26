import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { collectTags, loadManifests, narrowTags, parseTagList, planPasses, TckError } from './tck-select.mjs';

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
  const selected = narrowTags(collectTags(manifests), parseTagList(tags));
  return planPasses(selected);
}

function main() {
  const { values } = parseArgs({
    options: {
      tags: { type: 'string' },
      'dry-run': { type: 'boolean', default: false },
      'specs-dir': { type: 'string' },
    },
    strict: true,
  });
  const specsDir = values['specs-dir'] ? resolve(values['specs-dir']) : SPECS_ROOT;
  let passes;
  try {
    passes = planRun({ specsDir, tags: values.tags });
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

  const cli = createRequire(import.meta.url).resolve('@playwright/test/cli');
  let failed = false;
  for (const pass of passes) {
    const result = spawnSync(process.execPath, [cli, ...pass.args], { cwd: specsDir, stdio: 'inherit' });
    if (result.status !== 0) {
      failed = true;
    }
  }
  process.exit(failed ? 1 : 0);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
