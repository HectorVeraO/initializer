import { expect, test } from '@playwright/test';
import { expectEnvelope } from '../../tck-support';
import { bootstrapOperator } from '../../tck-support/actors';

const SESSION = '/api/v1/auth/session';

test(
  'an authenticated session request without traceparent carries matching and distinct trace ids',
  { tag: '@identity' },
  async ({ playwright }) => {
    const operator = await bootstrapOperator(playwright);
    const ids: string[] = [];
    for (let attempt = 0; attempt < 2; attempt += 1) {
      const response = await operator.request.get(SESSION);
      const envelope = await expectEnvelope(response, { status: 200 });
      expect(envelope.meta.traceId).toMatch(/^[0-9a-f]{32}$/);
      expect(response.headers()['x-trace-id']).toBe(envelope.meta.traceId);
      ids.push(envelope.meta.traceId);
    }
    expect(ids[0]).not.toBe(ids[1]);
    await operator.request.dispose();
  },
);

test('a sampled traceparent is continued on the session route without credentials', { tag: '@identity' }, async ({ request }) => {
  const traceId = '4bf92f3577b34da6a3ce929d0e0e4736';
  const response = await request.get(SESSION, { headers: { traceparent: `00-${traceId}-00f067aa0ba902b7-01` } });
  const envelope = await expectEnvelope(response, { status: 401, code: 'SESSION_INVALID' });
  expect(envelope.meta.traceId).toBe(traceId);
  expect(response.headers()['x-trace-id']).toBe(traceId);
});
