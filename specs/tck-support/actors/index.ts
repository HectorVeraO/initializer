import { strict as assert } from 'node:assert';
import { randomBytes } from 'node:crypto';
import type { APIRequestContext, APIResponse, PlaywrightWorkerArgs } from '@playwright/test';
import { expectEnvelope } from '../envelope';
import { kratosLoginAdapter } from './login.kratos';

/** The `playwright` worker fixture. */
export type Playwright = PlaywrightWorkerArgs['playwright'];

export type Role = 'admin' | 'member' | 'viewer';

/** A signed-in actor: its own cookie jar plus what the TCK knows about it. */
export interface Actor {
  request: APIRequestContext;
  email: string;
  password: string;
  accountId: string;
}

/** login.<idp>.ts implements this; login.kratos.ts drives the browser flow under /kratos/. */
export interface LoginAdapter {
  login(request: APIRequestContext, email: string, password: string): Promise<void>;
}

const BASE32_ALPHABET = 'abcdefghijklmnopqrstuvwxyz234567';
const SUFFIX_LENGTH = 6;
let cachedSuffix: string | undefined;

/** 6 lowercase base32 characters, fixed per worker run. */
export function runSuffix(): string {
  if (cachedSuffix === undefined) {
    const bytes = randomBytes(SUFFIX_LENGTH);
    cachedSuffix = Array.from(bytes, (byte) => BASE32_ALPHABET[byte % BASE32_ALPHABET.length]).join('');
  }
  return cachedSuffix;
}

function requiredEnv(name: string): string {
  const value = process.env[name];
  assert.ok(value, `${name} is required`);
  return value;
}

/** The origin every request goes to: the TCK runner's base URL. */
export function baseUrl(): string {
  return requiredEnv('TCK_BASE_URL');
}

/** Selected by TCK_LOGIN_ADAPTER; the default is kratos. */
export function loginAdapter(): LoginAdapter {
  const name = process.env.TCK_LOGIN_ADAPTER ?? 'kratos';
  assert.equal(name, 'kratos', `TCK_LOGIN_ADAPTER ${name} is not supported`);
  return kratosLoginAdapter;
}

/** A fresh context with its own cookie jar against the instance under test. */
export async function newContext(playwright: Playwright): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL: baseUrl() });
}

/** Asserts status, envelope keys, error.code, meta.traceId == X-Trace-Id; returns the error object. */
export async function expectError(
  response: APIResponse,
  status: number,
  code: string,
): Promise<{ code: string; message: string; issues: Array<{ field: string; code: string; message: string }> }> {
  const envelope = await expectEnvelope(response, { status, code });
  assert.ok(envelope.error, 'expectError: the envelope has no error');
  return envelope.error as { code: string; message: string; issues: Array<{ field: string; code: string; message: string }> };
}

/** Asserts 2xx envelope and returns data. */
export async function expectData<T>(response: APIResponse, status: number): Promise<T> {
  assert.ok(status >= 200 && status <= 299, 'expectData: status must be 2xx');
  const envelope = await expectEnvelope(response, { status });
  assert.notEqual(envelope.data, null, 'expectData: data is null');
  return envelope.data as T;
}

interface SessionAccountView {
  account: { id: string };
}

/** New context, adapter login, then GET /api/v1/auth/session (expects 200). */
export async function login(playwright: Playwright, email: string, password: string): Promise<Actor> {
  const request = await newContext(playwright);
  await loginAdapter().login(request, email, password);
  const session = await expectData<SessionAccountView>(await request.get('/api/v1/auth/session'), 200);
  return { request, email, password, accountId: session.account.id };
}

/** Signs in the Day-0 admin from GATBASE_BOOTSTRAP_ADMIN_EMAIL / _PASSWORD. */
export async function bootstrapOperator(playwright: Playwright): Promise<Actor> {
  const email = requiredEnv('GATBASE_BOOTSTRAP_ADMIN_EMAIL');
  const password = requiredEnv('GATBASE_BOOTSTRAP_ADMIN_PASSWORD');
  return login(playwright, email, password);
}

/** Cookie mutation with X-XSRF-TOKEN read from the jar's XSRF-TOKEN cookie. */
export async function mutate(
  actor: Actor,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
): Promise<APIResponse> {
  const state = await actor.request.storageState();
  const csrfCookie = state.cookies.find((cookie) => cookie.name === 'XSRF-TOKEN');
  assert.ok(csrfCookie, 'mutate: the actor holds no XSRF-TOKEN cookie');
  return actor.request.fetch(path, {
    method,
    headers: { 'X-XSRF-TOKEN': csrfCookie.value },
    data: body,
  });
}
