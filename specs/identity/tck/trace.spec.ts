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
