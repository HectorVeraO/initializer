import { isApiEnvelope } from './envelope.utils';

describe('isApiEnvelope', () => {
  it('accepts a success envelope', () => {
    const body = { data: { id: 'tenant-a' }, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: null };

    expect(isApiEnvelope(body)).toBe(true);
  });

  it('accepts a failure envelope with issues', () => {
    const body = { data: null, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: { code: 'VALIDATION_FAILED', message: 'invalid', issues: [] } };

    expect(isApiEnvelope(body)).toBe(true);
  });

  it('rejects bodies that are not the envelope', () => {
    expect(isApiEnvelope(null)).toBe(false);
    expect(isApiEnvelope('<html>Bad Gateway</html>')).toBe(false);
    expect(isApiEnvelope({ meta: null })).toBe(false);
    expect(isApiEnvelope({ meta: {} })).toBe(false);
  });
});
