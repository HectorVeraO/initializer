import { strict as assert } from 'node:assert';
import type { APIResponse } from '@playwright/test';

export interface ApiEnvelope<T> {
  data: T | null;
  meta: { traceId: string; timestamp: string };
  error: { code: string; message?: string; issues: unknown[] } | null;
}

const TRACE_ID_PATTERN = /^[0-9a-f]{32}$/;
const TIMESTAMP_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/;
const ALL_ZERO_TRACE_ID = '0'.repeat(32);

function isRecord(value: unknown): value is Record<string, unknown> {
  const isObject = typeof value === 'object' && value !== null && !Array.isArray(value);
  return isObject;
}

/**
 * Asserts the Gatbase wire envelope of an `/api/v1/` response and returns it.
 *
 * Checks the status, the exact `data`/`meta`/`error` keys, the trace id and its
 * `X-Trace-Id` header twin, the timestamp shape, `Cache-Control: no-store`, and
 * the error code plus `issues` array when `error` is set.
 */
export async function expectEnvelope(
  response: APIResponse,
  expected: { status: number; code?: string },
): Promise<ApiEnvelope<unknown>> {
  assert.equal(response.status(), expected.status, 'envelope: unexpected HTTP status');

  const body: unknown = await response.json();
  assert.ok(isRecord(body), 'envelope: body is not a JSON object');
  assert.deepEqual(Object.keys(body).sort(), ['data', 'error', 'meta'], 'envelope: keys must be exactly data, meta, error');

  const meta = body.meta;
  assert.ok(isRecord(meta), 'envelope: meta is not an object');
  const traceId = meta.traceId;
  assert.ok(typeof traceId === 'string' && TRACE_ID_PATTERN.test(traceId), 'envelope: meta.traceId is not 32 lowercase hex');
  assert.notEqual(traceId, ALL_ZERO_TRACE_ID, 'envelope: meta.traceId is all zeros');
  assert.equal(response.headers()['x-trace-id'], traceId, 'envelope: X-Trace-Id differs from meta.traceId');
  const timestamp = meta.timestamp;
  assert.ok(typeof timestamp === 'string' && TIMESTAMP_PATTERN.test(timestamp), 'envelope: meta.timestamp is not UTC RFC 3339 seconds');

  const cacheControl = response.headers()['cache-control'] ?? '';
  assert.ok(cacheControl.includes('no-store'), 'envelope: Cache-Control lacks no-store');

  const error = body.error;
  if (error === null || error === undefined) {
    assert.equal(expected.code, undefined, `envelope: expected error code ${expected.code} but error is null`);
  } else {
    assert.ok(isRecord(error), 'envelope: error is not an object');
    assert.notEqual(expected.code, undefined, 'envelope: error is set but no expected code was given');
    assert.equal(error.code, expected.code, 'envelope: unexpected error.code');
    assert.ok(Array.isArray(error.issues), 'envelope: error.issues is not an array');
  }

  return body as unknown as ApiEnvelope<unknown>;
}
