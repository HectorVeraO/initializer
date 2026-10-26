import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { loadManifests, TckError } from './tck-select.mjs';

const KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Quotes a value for a POSIX shell, escaping single quotes. */
export function shellQuote(value) {
  const escaped = String(value).replaceAll("'", "'\\''");
  return `'${escaped}'`;
}

/**
 * Merges every spec's `tckEnv` into sorted `export KEY='value'` lines.
 * Throws a TckError (exit 1) on an invalid key, a non-string value or a conflict.
 * @param {{ spec: string, tckEnv: Record<string, string> }[]} manifests
 * @returns {string[]}
 */
export function buildExports(manifests) {
  const merged = new Map();
  const problems = [];
  for (const manifest of manifests) {
    for (const [key, value] of Object.entries(manifest.tckEnv)) {
      if (!KEY_PATTERN.test(key)) {
        problems.push(`spec ${manifest.spec}: invalid env key ${JSON.stringify(key)}`);
        continue;
      }
      if (typeof value !== 'string') {
        problems.push(`spec ${manifest.spec}: env ${key} must be a string`);
        continue;
      }
      const previous = merged.get(key);
      if (previous === undefined) {
        merged.set(key, { spec: manifest.spec, value });
      } else if (previous.value !== value) {
        problems.push(`env ${key} conflicts: spec ${previous.spec} and spec ${manifest.spec} set different values`);
      }
    }
  }
  if (problems.length > 0) {
    throw new TckError(problems.join('\n'), 1);
  }
  const lines = [...merged.keys()].sort().map((key) => `export ${key}=${shellQuote(merged.get(key).value)}`);
  return lines;
}

function main() {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
  try {
    const lines = buildExports(loadManifests(root));
    if (lines.length > 0) {
      process.stdout.write(`${lines.join('\n')}\n`);
    }
  } catch (error) {
    if (error instanceof TckError) {
      process.stderr.write(`tck-env: ${error.message}\n`);
      process.exit(error.exitCode);
    }
    throw error;
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
