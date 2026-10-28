import { strict as assert } from 'node:assert';
import { expect, test } from '@playwright/test';
import type { APIResponse } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, loginAdapter, mutate, newContext } from '../../tck-support/actors';

interface SessionView {
  account: { id: string; role: string | null; isPlatformOperator: boolean };
  activeTenant: { slug: string } | null;
  memberships: Array<{ tenant: { slug: string }; role: string }>;
  flags: Record<string, boolean>;
}

const publicOrigin = (): string => process.env.APP_PUBLIC_BASE_URL ?? process.env.TCK_BASE_URL ?? '';

function setCookieHeaders(response: APIResponse): string[] {
  return response
    .headersArray()
    .filter((header) => header.name.toLowerCase() === 'set-cookie')
    .map((header) => header.value);
}

function expectNoStore(response: APIResponse): void {
  expect(response.headers()['cache-control'] ?? '').toContain('no-store');
}

test('CA-01 the bootstrap operator signs in and reads the Day-0 session', { tag: '@identity' }, async ({ playwright }) => {
  const request = await newContext(playwright);
  await loginAdapter().login(request, process.env.APP_BOOTSTRAP_ADMIN_EMAIL ?? '', process.env.APP_BOOTSTRAP_ADMIN_PASSWORD ?? '');

  const response = await request.get('/api/v1/auth/session');
  const session = await expectData<SessionView>(response, 200);

  expect(session.account.isPlatformOperator).toBe(true);
  expect(Array.isArray(session.memberships)).toBe(true);
  expect(typeof session.flags).toBe('object');
  expect(session.flags).not.toBeNull();
  expect(setCookieHeaders(response).some((value) => value.startsWith('XSRF-TOKEN='))).toBe(true);
  expectNoStore(response);
  await request.dispose();
});

test('CA-14 the session route refuses a missing or altered session cookie', { tag: '@identity' }, async ({ playwright, request }) => {
  await expectError(await request.get('/api/v1/auth/session'), 401, 'SESSION_INVALID');

  const operator = await bootstrapOperator(playwright);
  const state = await operator.request.storageState();
  const sessionCookie = state.cookies.find((cookie) => cookie.httpOnly && cookie.name !== 'XSRF-TOKEN' && !cookie.name.startsWith('ory_'));
  assert.ok(sessionCookie, 'the actor holds no BFF session cookie');
  const middle = Math.floor(sessionCookie.value.length / 2);
  const replacement = sessionCookie.value[middle] === 'A' ? 'B' : 'A';
  const altered = sessionCookie.value.slice(0, middle) + replacement + sessionCookie.value.slice(middle + 1);

  const tampered = await playwright.request.newContext({
    baseURL: process.env.TCK_BASE_URL,
    extraHTTPHeaders: { Cookie: `${sessionCookie.name}=${altered}` },
  });
  await expectError(await tampered.get('/api/v1/auth/session'), 401, 'SESSION_INVALID');
  await tampered.dispose();
  await operator.request.dispose();
});

test('CA-18 logout revokes the session, clears the cookies and blocks a re-issue', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const before = await operator.request.storageState();
  const kratosCookie = before.cookies.find((cookie) => cookie.name.startsWith('ory_kratos_session'));
  assert.ok(kratosCookie, 'the actor holds no Kratos session cookie');

  const response = await mutate(operator, 'POST', '/api/v1/auth/logout');
  const result = await expectData<{ logoutUrl: string }>(response, 200);

  expect(result.logoutUrl).toBe(`${publicOrigin()}/login`);
  const cleared = setCookieHeaders(response).filter((value) => /max-age=0/i.test(value) || /expires=thu, 01 jan 1970/i.test(value));
  expect(cleared.some((value) => value.startsWith('XSRF-TOKEN='))).toBe(true);
  expect(cleared.some((value) => value.startsWith(`${kratosCookie.name}=`))).toBe(true);
  expectNoStore(response);

  await expectError(await operator.request.get('/api/v1/auth/session'), 401, 'SESSION_INVALID');

  // Replaying the pre-logout Kratos cookie only fails when logout revoked it.
  const replay = await playwright.request.newContext({
    baseURL: process.env.TCK_BASE_URL,
    extraHTTPHeaders: {
      Cookie: `${kratosCookie.name}=${kratosCookie.value}`,
    },
  });
  await expectError(await replay.get('/api/v1/auth/session'), 401, 'SESSION_INVALID');
  await replay.dispose();
  await operator.request.dispose();
});

test('CA-19 the session and logout answers carry Cache-Control no-store', { tag: '@identity' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  const session = await operator.request.get('/api/v1/auth/session');
  expect(session.status()).toBe(200);
  expectNoStore(session);
  const logout = await mutate(operator, 'POST', '/api/v1/auth/logout');
  expect(logout.status()).toBe(200);
  expectNoStore(logout);
  await operator.request.dispose();
});

test('a method no auth route accepts answers an envelope 404 ROUTE_NOT_FOUND', { tag: '@identity' }, async ({ request }) => {
  await expectError(await request.delete('/api/v1/auth/session'), 404, 'ROUTE_NOT_FOUND');
});
