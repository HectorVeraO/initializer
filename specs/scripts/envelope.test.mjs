import { test } from 'node:test';
import assert from 'node:assert/strict';
import { expectEnvelope } from '../tck-support/envelope.ts';

const TRACE_ID = '4bf92f3577b34da6a3ce929d0e0e4736';

function fakeResponse({ status = 200, headers, body }) {
  const lowered = { 'x-trace-id': TRACE_ID, 'cache-control': 'no-store', ...headers };
  return {
    status: () => status,
    headers: () => lowered,
    json: async () => body,
  };
}

function okBody(overrides = {}) {
  return {
    data: { id: 'abc' },
    meta: { traceId: TRACE_ID, timestamp: '2026-01-02T03:04:05Z' },
    error: null,
    ...overrides,
  };
}

test('a well-formed success envelope is returned', async () => {
  const envelope = await expectEnvelope(fakeResponse({ body: okBody() }), { status: 200 });
  assert.deepEqual(envelope.data, { id: 'abc' });
});

test('a well-formed error envelope with issues passes', async () => {
  const body = okBody({ data: null, error: { code: 'VALIDATION_FAILED', message: 'bad', issues: [] } });
  await expectEnvelope(fakeResponse({ status: 422, body }), { status: 422, code: 'VALIDATION_FAILED' });
});

test('a wrong status fails', async () => {
  await assert.rejects(expectEnvelope(fakeResponse({ status: 500, body: okBody() }), { status: 200 }), /unexpected HTTP status/);
});

test('an extra or missing top-level key fails', async () => {
  await assert.rejects(expectEnvelope(fakeResponse({ body: okBody({ extra: 1 }) }), { status: 200 }), /keys must be exactly/);
  const { error: _error, ...withoutError } = okBody();
  await assert.rejects(expectEnvelope(fakeResponse({ body: withoutError }), { status: 200 }), /keys must be exactly/);
});

test('an all-zero trace id fails', async () => {
  const zero = '0'.repeat(32);
  const body = okBody({ meta: { traceId: zero, timestamp: '2026-01-02T03:04:05Z' } });
  await assert.rejects(
    expectEnvelope(fakeResponse({ body, headers: { 'x-trace-id': zero } }), { status: 200 }),
    /all zeros/,
  );
});

test('a header and body trace id mismatch fails', async () => {
  const other = 'a'.repeat(32);
  await assert.rejects(
    expectEnvelope(fakeResponse({ body: okBody(), headers: { 'x-trace-id': other } }), { status: 200 }),
    /X-Trace-Id/,
  );
});

test('a malformed trace id or timestamp fails', async () => {
  const badTrace = okBody({ meta: { traceId: 'XYZ', timestamp: '2026-01-02T03:04:05Z' } });
  await assert.rejects(expectEnvelope(fakeResponse({ body: badTrace, headers: { 'x-trace-id': 'XYZ' } }), { status: 200 }), /32 lowercase hex/);
  const badTime = okBody({ meta: { traceId: TRACE_ID, timestamp: '2026-01-02 03:04:05' } });
  await assert.rejects(expectEnvelope(fakeResponse({ body: badTime }), { status: 200 }), /timestamp/);
});

test('a missing no-store fails', async () => {
  await assert.rejects(
    expectEnvelope(fakeResponse({ body: okBody(), headers: { 'cache-control': 'max-age=60' } }), { status: 200 }),
    /no-store/,
  );
});

test('an error code mismatch or missing issues array fails', async () => {
  const wrongCode = okBody({ data: null, error: { code: 'OTHER', issues: [] } });
  await assert.rejects(expectEnvelope(fakeResponse({ status: 422, body: wrongCode }), { status: 422, code: 'VALIDATION_FAILED' }), /unexpected error\.code/);
  const noIssues = okBody({ data: null, error: { code: 'VALIDATION_FAILED' } });
  await assert.rejects(expectEnvelope(fakeResponse({ status: 422, body: noIssues }), { status: 422, code: 'VALIDATION_FAILED' }), /issues is not an array/);
});

test('an expected code with a null error fails', async () => {
  await assert.rejects(expectEnvelope(fakeResponse({ body: okBody() }), { status: 200, code: 'X' }), /error is null/);
});
