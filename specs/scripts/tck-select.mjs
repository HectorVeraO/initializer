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
 * tckTags and cooperatesWith must be arrays when present (TckError exit 2); a
 * cooperatesWith item must match the tag pattern and must not be one of the
 * spec's own tckTags (TckError exit 2).
 * @returns {{ spec: string, tckTags: string[], tckEnv: Record<string, string>, cooperatesWith: string[] }[]}
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
    const spec = typeof raw.spec === 'string' ? raw.spec : folder;
    const tckTags = raw.tckTags ?? [];
    if (raw.cooperatesWith !== undefined && !Array.isArray(raw.cooperatesWith)) {
      throw new TckError(`cooperatesWith must be an array in ${path} (spec ${spec})`, 2);
    }
    const cooperatesWith = raw.cooperatesWith ?? [];
    for (const tag of cooperatesWith) {
      if (typeof tag !== 'string' || !TAG_PATTERN.test(tag)) {
        throw new TckError(`invalid cooperatesWith tag ${JSON.stringify(tag)} in spec ${spec}`, 2);
      }
      if (tckTags.includes(tag)) {
        throw new TckError(`cooperatesWith tag ${tag} is one of the own tckTags of spec ${spec}`, 2);
      }
    }
    manifests.push({
      spec,
      tckTags,
      tckEnv: raw.tckEnv && typeof raw.tckEnv === 'object' ? raw.tckEnv : {},
      cooperatesWith,
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
 * Cooperating tags whose spec is not installed: the union of every manifest's
 * cooperatesWith minus `installed`, sorted bytewise, without duplicates.
 * @param {{ cooperatesWith: string[] }[]} manifests
 * @param {string[]} installed
 * @returns {string[]}
 */
export function collectInvertTags(manifests, installed) {
  if (!Array.isArray(manifests) || !Array.isArray(installed)) {
    throw new TypeError('collectInvertTags: manifests and installed must be arrays');
  }
  const missing = new Set();
  for (const manifest of manifests) {
    for (const tag of manifest.cooperatesWith ?? []) {
      if (!installed.includes(tag)) {
        missing.add(tag);
      }
    }
  }
  const sorted = [...missing].sort();
  return sorted;
}

/**
 * Plans the Playwright passes. Pass 1 holds every tag except `@feature-flags` and
 * inverts `@feature-flags` plus invertTags; pass 2 runs `@feature-flags` alone on
 * one worker and inverts invertTags when there are any.
 * @param {string[]} tags selected tags
 * @param {string[]} [invertTags] uninstalled cooperating tags
 * @returns {{ name: string, args: string[] }[]}
 */
export function planPasses(tags, invertTags = []) {
  const hasFlags = tags.includes(FLAGS_TAG);
  const rest = tags.filter((tag) => tag !== FLAGS_TAG);
  const passes = [];
  if (rest.length > 0) {
    const mainInvert = [FLAGS_TAG, ...invertTags.filter((tag) => tag !== FLAGS_TAG)];
    passes.push({
      name: 'main',
      args: ['test', '--grep', tagPattern(rest), '--grep-invert', tagPattern(mainInvert)],
    });
  }
  if (hasFlags) {
    const args = ['test', '--grep', tagPattern([FLAGS_TAG])];
    if (invertTags.length > 0) {
      args.push('--grep-invert', tagPattern(invertTags));
    }
    args.push('--workers', '1');
    passes.push({ name: 'feature-flags', args });
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
