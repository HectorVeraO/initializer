import { RuntimeConfig } from './runtime-config.types';

export type RuntimeConfigValidation = { ok: true; config: RuntimeConfig } | { ok: false; missingFields: string[] };

function asRecord(value: unknown): Record<string, unknown> {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }

  return {};
}

/**
 * Validates the parsed runtime config. Every problem is accumulated so the
 * error page can name all of them at once; there are no fallback values.
 */
export function validateRuntimeConfig(raw: unknown): RuntimeConfigValidation {
  const root = asRecord(raw);

  const missingFields: string[] = [];

  if (typeof root['apiUrl'] !== 'string') {
    missingFields.push('apiUrl');
  }

  if (missingFields.length > 0) {
    return { ok: false, missingFields };
  }

  const apiUrl = (root['apiUrl'] as string).trim().replace(/\/+$/, '');

  const config: RuntimeConfig = { apiUrl };

  return { ok: true, config };
}
