import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate } from '../../tck-support/actors';
import type { Actor } from '../../tck-support/actors';

const FLAGS = '/api/v1/feature-flags';
const RELEASE_KEY = 'new_settings_ui';
const OPS_KEY = 'example_outbound_enabled';
const UNKNOWN_TENANT = '00000000-0000-4000-8000-0000000000ff';

interface SessionView {
  flags: Record<string, boolean>;
}

interface FlagView {
  key: string;
  type: string;
  default: boolean;
  expiresAt: string | null;
  description: string;
  globalOverride: { enabled: boolean; updatedAt: string } | null;
  tenantOverrides: Array<{
    tenantId: string;
    enabled: boolean;
    updatedAt: string;
  }>;
}

interface FlagPage {
  items: FlagView[];
  page: number;
  size: number;
  totalItems: number;
}

test.describe.configure({ mode: 'serial' });

let operator: Actor;

async function readFlags(actor: Actor): Promise<Record<string, boolean>> {
  const session = await expectData<SessionView>(await actor.request.get('/api/v1/auth/session'), 200);
  return session.flags;
}

function tenantPath(key: string, tenantId: string): string {
  return `${FLAGS}/${key}/tenants/${tenantId}`;
}

async function listFlags(actor: Actor): Promise<FlagPage> {
  return expectData<FlagPage>(await actor.request.get(`${FLAGS}?size=100`), 200);
}

test.beforeAll(async ({ playwright }) => {
  operator = await bootstrapOperator(playwright);
  for (const key of [RELEASE_KEY, OPS_KEY]) {
    await expectData<FlagView>(await mutate(operator, 'DELETE', `${FLAGS}/${key}`), 200);
  }
});

test('F4 the session map holds the manifest keys only', { tag: '@feature-flags' }, async () => {
  const flags = await readFlags(operator);
  expect(Object.keys(flags).sort()).toEqual([OPS_KEY, RELEASE_KEY]);
  for (const value of Object.values(flags)) {
    expect(typeof value).toBe('boolean');
  }
});

test('F6 an undeclared key is not defined in the manifest', { tag: '@feature-flags' }, async () => {
  const code = 'FLAG_NOT_DEFINED_IN_MANIFEST';
  await expectError(await mutate(operator, 'PUT', `${FLAGS}/flag_ghost`, { enabled: true }), 404, code);
  await expectError(await mutate(operator, 'DELETE', `${FLAGS}/flag_ghost`), 404, code);
});

test('F7 the body must carry a boolean enabled', { tag: '@feature-flags' }, async () => {
  const path = `${FLAGS}/${RELEASE_KEY}`;
  const wrongType = await expectError(await mutate(operator, 'PUT', path, { enabled: 'yes' }), 422, 'VALIDATION_FAILED');
  expect(wrongType.issues).toEqual([expect.objectContaining({ field: 'enabled', code: 'FIELD_TYPE_INVALID' })]);

  const missing = await expectError(await mutate(operator, 'PUT', path, {}), 422, 'VALIDATION_FAILED');
  expect(missing.issues).toEqual([expect.objectContaining({ field: 'enabled', code: 'FIELD_REQUIRED' })]);
});

test('F8 an unknown or malformed tenant is not found', { tag: '@feature-flags' }, async () => {
  const unknown = tenantPath(RELEASE_KEY, UNKNOWN_TENANT);
  await expectError(await mutate(operator, 'PUT', unknown, { enabled: true }), 404, 'RESOURCE_NOT_FOUND');
  const malformed = tenantPath(RELEASE_KEY, 'not-a-uuid');
  await expectError(await mutate(operator, 'PUT', malformed, { enabled: true }), 404, 'RESOURCE_NOT_FOUND');
});

test('F9 the operator listing shows every flag', { tag: '@feature-flags' }, async () => {
  const page = await listFlags(operator);
  expect(page.totalItems).toBe(2);
  expect(page.items.map((item) => item.key)).toEqual([OPS_KEY, RELEASE_KEY]);

  const [ops, release] = page.items;
  expect(release).toMatchObject({
    type: 'release',
    default: false,
    expiresAt: '2099-12-31',
    globalOverride: null,
  });
  expect(ops).toMatchObject({
    type: 'ops',
    default: true,
    expiresAt: null,
    globalOverride: null,
  });
});

test("F11 a global override reaches the operator's session map and clearing it falls back", { tag: '@feature-flags' }, async () => {
  try {
    await expectData<FlagView>(
      await mutate(operator, 'PUT', `${FLAGS}/${RELEASE_KEY}`, {
        enabled: true,
      }),
      200,
    );
    expect((await readFlags(operator))[RELEASE_KEY]).toBe(true);
  } finally {
    const cleared = await expectData<FlagView>(await mutate(operator, 'DELETE', `${FLAGS}/${RELEASE_KEY}`), 200);
    expect(cleared.globalOverride).toBeNull();
  }
  expect((await readFlags(operator))[RELEASE_KEY]).toBe(false);
});
