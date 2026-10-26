import { strict as assert } from 'node:assert';
import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { APIResponse } from '@playwright/test';
import { bootstrapOperator, expectData, expectError } from '../../tck-support/actors';
import type { Actor } from '../../tck-support/actors';

interface SessionView {
  account: { id: string };
  activeTenant: { id: string; slug: string } | null;
}

const SWITCH = '/api/v1/auth/switch-tenant';
const REFRESH = '/api/v1/auth/refresh';

async function xsrfValue(actor: Actor): Promise<string> {
  const state = await actor.request.storageState();
  const cookie = state.cookies.find((item) => item.name === 'XSRF-TOKEN');
  assert.ok(cookie, 'the actor holds no XSRF-TOKEN cookie');
  return cookie.value;
}

function expectNoStore(response: APIResponse): void {
  expect(response.headers()['cache-control'] ?? '').toContain('no-store');
}

async function activeTenant(actor: Actor): Promise<{ id: string; slug: string }> {
  const session = await expectData<SessionView>(await actor.request.get('/api/v1/auth/session'), 200);
  assert.ok(session.activeTenant, 'the actor has no active tenant');
  return session.activeTenant;
}

test('CA-17 refresh keeps the tenant and the CSRF value', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await activeTenant(operator);
  const csrfBefore = await xsrfValue(operator);

  const refreshed = await operator.request.post(REFRESH, { headers: { 'X-XSRF-TOKEN': csrfBefore } });
  const session = await expectData<SessionView>(refreshed, 200);

  expect(session.activeTenant?.slug).toBe('acme');
  expectNoStore(refreshed);
  expect(await xsrfValue(operator)).toBe(csrfBefore);

  const switched = await operator.request.post(SWITCH, {
    headers: { 'X-XSRF-TOKEN': csrfBefore },
    data: { tenantId: tenant.id },
  });
  const view = await expectData<SessionView>(switched, 200);
  expect(view.activeTenant?.slug).toBe('acme');
  expectNoStore(switched);
  await operator.request.dispose();
});

test('CA-13 switch-tenant refuses a missing, wrong or cross-site CSRF token', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await activeTenant(operator);
  const csrf = await xsrfValue(operator);
  const data = { tenantId: tenant.id };

  await expectError(await operator.request.post(SWITCH, { data }), 403, 'CSRF_TOKEN_INVALID');
  await expectError(
    await operator.request.post(SWITCH, { headers: { 'X-XSRF-TOKEN': `${csrf}x` }, data }),
    403,
    'CSRF_TOKEN_INVALID',
  );
  await expectError(
    await operator.request.post(SWITCH, {
      headers: { 'X-XSRF-TOKEN': csrf, 'Sec-Fetch-Site': 'cross-site', Origin: 'https://evil.example.com' },
      data,
    }),
    403,
    'CSRF_TOKEN_INVALID',
  );

  expect((await activeTenant(operator)).slug).toBe('acme');
  await operator.request.dispose();
});

test('CA-13 a token of another sign-in is refused as the CSRF header', { tag: '@core-auth' }, async ({ playwright }) => {
  const actorA = await bootstrapOperator(playwright);
  const actorB = await bootstrapOperator(playwright);
  const tenant = await activeTenant(actorB);

  const response = await actorB.request.post(SWITCH, {
    headers: { 'X-XSRF-TOKEN': await xsrfValue(actorA) },
    data: { tenantId: tenant.id },
  });

  await expectError(response, 403, 'CSRF_TOKEN_INVALID');
  await actorA.request.dispose();
  await actorB.request.dispose();
});

test('switch-tenant answers every body problem as 422 and an unknown tenant as 403', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const csrf = await xsrfValue(operator);
  const post = (data: unknown): Promise<APIResponse> =>
    operator.request.post(SWITCH, { headers: { 'X-XSRF-TOKEN': csrf }, data });

  const cases: Array<[unknown, string]> = [
    [{}, 'FIELD_REQUIRED'],
    [{ tenantId: 5 }, 'FIELD_TYPE_INVALID'],
    [{ tenantId: 'not-a-uuid' }, 'FIELD_INVALID_FORMAT'],
  ];
  for (const [body, code] of cases) {
    const error = await expectError(await post(body), 422, 'VALIDATION_FAILED');
    expect(error.issues).toEqual([expect.objectContaining({ field: 'tenantId', code })]);
  }

  await expectError(await post({ tenantId: randomUUID() }), 403, 'TENANT_ACCESS_DENIED');
  expect((await activeTenant(operator)).slug).toBe('acme');
  await operator.request.dispose();
});
