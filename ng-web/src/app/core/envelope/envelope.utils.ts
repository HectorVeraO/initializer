import type { ApiEnvelope } from './envelope.types';

export function isApiEnvelope(body: unknown): body is ApiEnvelope {
  if (typeof body !== 'object' || body === null) {
    return false;
  }

  const record = body as Record<string, unknown>;

  const meta = record['meta'];

  return typeof meta === 'object' && meta !== null && 'traceId' in meta;
}
