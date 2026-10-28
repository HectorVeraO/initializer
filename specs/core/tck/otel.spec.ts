import { expect, test } from '@playwright/test';
import { expectEnvelope } from '../../tck-support';

const UNKNOWN_ROUTE = '/api/v1/route-that-does-not-exist';
const ALL_ZERO_TRACE_ID = '0'.repeat(32);

test('without traceparent the body and header carry the same non-zero trace id', { tag: '@otel' }, async ({ request }) => {
  const response = await request.get(UNKNOWN_ROUTE);
  const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  expect(envelope.meta.traceId).toBe(response.headers()['x-trace-id']);
  expect(envelope.meta.traceId).not.toBe(ALL_ZERO_TRACE_ID);
});

test('a valid traceparent is continued', { tag: '@otel' }, async ({ request }) => {
  const traceId = '4bf92f3577b34da6a3ce929d0e0e4736';
  const response = await request.get(UNKNOWN_ROUTE, {
    headers: { traceparent: `00-${traceId}-00f067aa0ba902b7-01` },
  });
  const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  expect(envelope.meta.traceId).toBe(traceId);
  expect(response.headers()['x-trace-id']).toBe(traceId);
});

test('an unsampled parent is continued too', { tag: '@otel' }, async ({ request }) => {
  const traceId = '4bf92f3577b34da6a3ce929d0e0e4737';
  const response = await request.get(UNKNOWN_ROUTE, {
    headers: { traceparent: `00-${traceId}-00f067aa0ba902b7-00` },
  });
  const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  expect(envelope.meta.traceId).toBe(traceId);
  expect(response.headers()['x-trace-id']).toBe(traceId);
});

test('a malformed traceparent starts a new trace', { tag: '@otel' }, async ({ request }) => {
  const response = await request.get(UNKNOWN_ROUTE, { headers: { traceparent: 'not-a-traceparent' } });
  const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  expect(envelope.meta.traceId).toMatch(/^[0-9a-f]{32}$/);
  expect(envelope.meta.traceId).toBe(response.headers()['x-trace-id']);
  expect(envelope.meta.traceId).not.toBe(ALL_ZERO_TRACE_ID);
});

test('an all-zero trace id in traceparent starts a fresh valid trace', { tag: '@otel' }, async ({ request }) => {
  const response = await request.get(UNKNOWN_ROUTE, {
    headers: { traceparent: `00-${ALL_ZERO_TRACE_ID}-00f067aa0ba902b7-01` },
  });
  const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  expect(envelope.meta.traceId).toMatch(/^[0-9a-f]{32}$/);
  expect(envelope.meta.traceId).not.toBe(ALL_ZERO_TRACE_ID);
  expect(response.headers()['x-trace-id']).toBe(envelope.meta.traceId);
});

test('two requests without traceparent get distinct trace ids', { tag: '@otel' }, async ({ request }) => {
  const ids: string[] = [];
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const response = await request.get(UNKNOWN_ROUTE);
    const envelope = await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
    expect(response.headers()['x-trace-id']).toBe(envelope.meta.traceId);
    ids.push(envelope.meta.traceId);
  }
  expect(ids[0]).not.toBe(ids[1]);
});
