import { firstValueFrom, of } from 'rxjs';
import { UnexpectedResponseError, isApiEnvelope, unwrapData } from './envelope.utils';
import type { ApiEnvelope } from './envelope.types';

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

describe('unwrapData', () => {
  const meta = { traceId: '0af7651916cd43dd8448eb211c80319c', timestamp: '2026-01-01T00:00:00Z' };

  it('maps an envelope to its data', async () => {
    const body: ApiEnvelope<{ id: string }> = { data: { id: 'tenant-a' }, meta, error: null };

    await expect(firstValueFrom(of(body).pipe(unwrapData()))).resolves.toEqual({ id: 'tenant-a' });
  });

  it('passes data null as null', async () => {
    const body: ApiEnvelope<string> = { data: null, meta, error: null };

    await expect(firstValueFrom(of(body).pipe(unwrapData()))).resolves.toBeNull();
  });

  it('errors with UnexpectedResponseError for a body that is not an envelope', async () => {
    const html = of('<html>Bad Gateway</html>' as unknown as ApiEnvelope<string>);

    await expect(firstValueFrom(html.pipe(unwrapData()))).rejects.toBeInstanceOf(UnexpectedResponseError);
  });

  it('errors for a null body', async () => {
    const empty = of(null as unknown as ApiEnvelope<string>);

    await expect(firstValueFrom(empty.pipe(unwrapData()))).rejects.toBeInstanceOf(UnexpectedResponseError);
  });
});
