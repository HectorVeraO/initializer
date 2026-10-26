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

/** The synthetic password every seeded account signs in with. */
export const SEEDED_PASSWORD = 'tck-password-0001';

export interface SeededTenant {
  id: string;
  slug: string;
  admin: Actor;
  /** Keyed by the label passed in SeedOptions.members. */
  members: Record<string, Actor>;
}

export interface SeedOptions {
  /** Slug base; the helper appends -<runSuffix>. */
  slugBase: string;
  members?: Array<{ label: string; role: Role }>;
}

interface TenantCreatedView {
  tenant: { id: string; slug: string };
  invitation: { id: string };
  invitationUrl: string;
}

/** Reads the token from the fragment of an invitation URL (`...#token=<token>`). */
export function tokenFromInvitationUrl(invitationUrl: string): string {
  const fragment = new URL(invitationUrl).hash;
  const prefix = '#token=';
  assert.ok(fragment.startsWith(prefix), 'the invitation URL carries no #token= fragment');
  return fragment.slice(prefix.length);
}

async function switchTo(actor: Actor, tenantId: string): Promise<void> {
  const response = await mutate(actor, 'POST', '/api/v1/auth/switch-tenant', { tenantId });
  await expectData<unknown>(response, 200);
}

/** POST /api/v1/invitations/accept-new, then login and switch-tenant onto the tenant. */
export async function joinAsNewAccount(playwright: Playwright, token: string, email: string, tenantId: string): Promise<Actor> {
  const anonymous = await newContext(playwright);
  const accepted = await anonymous.post('/api/v1/invitations/accept-new', {
    data: { token, password: SEEDED_PASSWORD, fullName: 'TCK Seeded User' },
  });
  await expectData<unknown>(accepted, 201);
  await anonymous.dispose();

  const actor = await login(playwright, email, SEEDED_PASSWORD);
  await switchTo(actor, tenantId);
  return actor;
}

/** Admin invites email with role; returns the token read from invitationUrl's fragment. */
export async function invite(admin: Actor, email: string, role: Role): Promise<{ invitationId: string; token: string }> {
  const response = await mutate(admin, 'POST', '/api/v1/invitations', { email, role });
  const created = await expectData<{ invitation: { id: string }; invitationUrl: string }>(response, 201);
  return { invitationId: created.invitation.id, token: tokenFromInvitationUrl(created.invitationUrl) };
}

/**
 * Operator POST /tenants with adminEmail, accept-new from the fragment, admin login,
 * admin switch-tenant onto it, then each member invited, accept-new, login, switch.
 * Emails are `<label>-<runSuffix>@example.com`; the admin's label is `<slugBase>-admin`.
 */
export async function seedTenant(playwright: Playwright, operator: Actor, options: SeedOptions): Promise<SeededTenant> {
  const suffix = runSuffix();
  const slug = `${options.slugBase}-${suffix}`;
  const adminEmail = `${options.slugBase}-admin-${suffix}@example.com`;

  const response = await mutate(operator, 'POST', '/api/v1/tenants', { slug, name: slug, adminEmail });
  const created = await expectData<TenantCreatedView>(response, 201);
  const tenantId = created.tenant.id;
  const admin = await joinAsNewAccount(playwright, tokenFromInvitationUrl(created.invitationUrl), adminEmail, tenantId);

  const members: Record<string, Actor> = {};
  for (const member of options.members ?? []) {
    const email = `${member.label}-${suffix}@example.com`;
    const { token } = await invite(admin, email, member.role);
    members[member.label] = await joinAsNewAccount(playwright, token, email, tenantId);
  }
  return { id: tenantId, slug, admin, members };
}
