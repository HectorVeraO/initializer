import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

export const FLAGS_TAG = '@feature-flags';
const TAG_PATTERN = /^@[a-z][a-z0-9-]*$/;

/** Error carrying the process exit code the CLI should use. */
export class TckError extends Error {
  constructor(message, exitCode) {
    super(message);
    this.name = 'TckError';
    this.exitCode = exitCode;
  }
}

/**
 * Reads every `<root>/<spec>/spec.manifest.json`, sorted by spec folder name.
 * @param {string} root directory holding the spec folders
 * @returns {{ spec: string, tckTags: string[], tckEnv: Record<string, string> }[]}
 */
export function loadManifests(root) {
  if (typeof root !== 'string' || root === '') {
    throw new TypeError('loadManifests: root must be a non-empty string');
  }
  if (!existsSync(root)) {
    return [];
  }
  const folders = readdirSync(root, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();
  const manifests = [];
  for (const folder of folders) {
    const path = join(root, folder, 'spec.manifest.json');
    if (!existsSync(path)) {
      continue;
    }
    const raw = JSON.parse(readFileSync(path, 'utf8'));
    if (raw.tckTags !== undefined && !Array.isArray(raw.tckTags)) {
      throw new TckError(`tckTags must be an array in ${path}`, 2);
    }
    manifests.push({
      spec: typeof raw.spec === 'string' ? raw.spec : folder,
      tckTags: raw.tckTags ?? [],
      tckEnv: raw.tckEnv && typeof raw.tckEnv === 'object' ? raw.tckEnv : {},
    });
  }
  return manifests;
}

/**
 * Installed tags in manifest order, without duplicates.
 * @param {{ spec: string, tckTags: string[] }[]} manifests
 * @returns {string[]}
 */
export function collectTags(manifests) {
  if (!Array.isArray(manifests)) {
    throw new TypeError('collectTags: manifests must be an array');
  }
  const tags = [];
  for (const manifest of manifests) {
    for (const tag of manifest.tckTags) {
      if (typeof tag !== 'string' || !TAG_PATTERN.test(tag)) {
        throw new TckError(`invalid tag ${JSON.stringify(tag)} in spec ${manifest.spec}`, 2);
      }
      if (!tags.includes(tag)) {
        tags.push(tag);
      }
    }
  }
  return tags;
}

/**
 * Narrows the installed tags to the requested subset; an uninstalled tag is an error (exit 2).
 * @param {string[]} installed
 * @param {string[] | undefined} requested
 * @returns {string[]}
 */
export function narrowTags(installed, requested) {
  if (requested === undefined) {
    return installed;
  }
  if (requested.length === 0) {
    throw new TckError('--tags needs at least one tag', 2);
  }
  const unknown = requested.filter((tag) => !installed.includes(tag));
  if (unknown.length > 0) {
    throw new TckError(
      `tag not installed: ${unknown.join(', ')} (installed: ${installed.join(', ') || 'none'})`,
      2,
    );
  }
  return installed.filter((tag) => requested.includes(tag));
}

/** Escapes a literal for use inside a RegExp source. */
export function escapeRegExp(text) {
  const escaped = text.replace(/[.*+?^${}()|[\]\\/-]/g, '\\$&');
  return escaped;
}

/**
 * Anchored pattern: a tag never matches a longer tag that shares its prefix.
 * @param {string[]} tags
 * @returns {string}
 */
export function tagPattern(tags) {
  if (tags.length === 0) {
    throw new TckError('no tags to select', 2);
  }
  const alternatives = tags.map(escapeRegExp).join('|');
  const pattern = `(${alternatives})(?![\\w-])`;
  return pattern;
}

/**
 * Plans the Playwright passes for a tag set. Pass 1 holds every tag except
 * `@feature-flags`; pass 2 runs `@feature-flags` alone on one worker.
 * @param {string[]} tags
 * @returns {{ name: string, args: string[] }[]}
 */
export function planPasses(tags) {
  const hasFlags = tags.includes(FLAGS_TAG);
  const rest = tags.filter((tag) => tag !== FLAGS_TAG);
  const passes = [];
  if (rest.length > 0) {
    passes.push({
      name: 'main',
      args: ['test', '--grep', tagPattern(rest), '--grep-invert', tagPattern([FLAGS_TAG])],
    });
  }
  if (hasFlags) {
    passes.push({
      name: 'feature-flags',
      args: ['test', '--grep', tagPattern([FLAGS_TAG]), '--workers', '1'],
    });
  }
  if (passes.length === 0) {
    throw new TckError('no tags to select', 2);
  }
  return passes;
}

/**
 * Parses the value of `--tags @a,@b` into a list, or undefined when absent.
 * @param {string | undefined} value
 * @returns {string[] | undefined}
 */
export function parseTagList(value) {
  if (value === undefined) {
    return undefined;
  }
  const tags = value
    .split(',')
    .map((tag) => tag.trim())
    .filter((tag) => tag !== '');
  return tags;
}
