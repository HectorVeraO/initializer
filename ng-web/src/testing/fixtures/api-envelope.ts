/**
 * Builds the body of an API error response (`ApiEnvelopeError`) for specs.
 */
export function errorEnvelope(code: string, issues: unknown[] = [], traceId = 'abc'): object {
  return { data: null, meta: { traceId, timestamp: '2026-01-01T00:00:00Z' }, error: { code, message: 'text', issues } };
}
