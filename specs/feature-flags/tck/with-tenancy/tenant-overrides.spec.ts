import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate } from '../../../tck-support/actors';
import { invite, seedTenant } from '../../../tck-support/actors/tenants';
import type { Actor } from '../../../tck-support/actors';
import type { SeededTenant } from '../../../tck-support/actors/tenants';

const FLAGS = '/api/v1/feature-flags';
const RELEASE_KEY = 'new_settings_ui';
const OPS_KEY = 'example_outbound_enabled';

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
let tenantA: SeededTenant;
let tenantB: SeededTenant;
let tenantC: SeededTenant;
let tenantD: SeededTenant;
// The admin of tenant-a, who also joined tenant-b as an admin (F3).
let twoTenantUser: Actor;

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
  tenantA = await seedTenant(playwright, operator, { slugBase: 'tenant-a' });
  tenantB = await seedTenant(playwright, operator, { slugBase: 'tenant-b' });
  tenantC = await seedTenant(playwright, operator, { slugBase: 'tenant-c' });
  tenantD = await seedTenant(playwright, operator, { slugBase: 'tenant-d' });

  // tenant-b's admin invites tenant-a's admin as an admin; that actor accepts.
  const { token } = await invite(tenantB.admin, tenantA.admin.email, 'admin');
  const accepted = await mutate(tenantA.admin, 'POST', '/api/v1/invitations/accept', { token });
  await expectData<unknown>(accepted, 200);
  twoTenantUser = tenantA.admin;
  // Accepting may leave another tenant active; F1 reads tenant-a's map.
  await expectData<SessionView>(
    await mutate(twoTenantUser, 'POST', '/api/v1/auth/switch-tenant', {
      tenantId: tenantA.id,
    }),
    200,
  );
});

test('F1 three levels resolve per tenant', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const setA = await mutate(operator, 'PUT', tenantPath(RELEASE_KEY, tenantA.id), { enabled: true });
  await expectData<FlagView>(setA, 200);
  const setB = await mutate(operator, 'PUT', tenantPath(OPS_KEY, tenantB.id), { enabled: false });
  await expectData<FlagView>(setB, 200);

  expect(await readFlags(tenantA.admin)).toEqual({
    example_outbound_enabled: true,
    new_settings_ui: true,
  });
  expect(await readFlags(tenantB.admin)).toEqual({
    example_outbound_enabled: false,
    new_settings_ui: false,
  });
});

test('F2 a global override yields to a tenant override', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  await expectData<FlagView>(
    await mutate(operator, 'PUT', tenantPath(RELEASE_KEY, tenantC.id), {
      enabled: false,
    }),
    200,
  );

  try {
    await expectData<FlagView>(
      await mutate(operator, 'PUT', `${FLAGS}/${RELEASE_KEY}`, {
        enabled: true,
      }),
      200,
    );
    expect((await readFlags(tenantD.admin))[RELEASE_KEY]).toBe(true);
    expect((await readFlags(tenantC.admin))[RELEASE_KEY]).toBe(false);
  } finally {
    const cleared = await expectData<FlagView>(await mutate(operator, 'DELETE', `${FLAGS}/${RELEASE_KEY}`), 200);
    expect(cleared.globalOverride).toBeNull();
  }
  expect((await readFlags(tenantD.admin))[RELEASE_KEY]).toBe(false);
});

test('F3 switch-tenant returns the map of the target tenant', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const toB = await mutate(twoTenantUser, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenantB.id });
  const sessionB = await expectData<SessionView>(toB, 200);
  expect(sessionB.flags).toEqual({
    example_outbound_enabled: false,
    new_settings_ui: false,
  });

  const toA = await mutate(twoTenantUser, 'POST', '/api/v1/auth/switch-tenant', { tenantId: tenantA.id });
  const sessionA = await expectData<SessionView>(toA, 200);
  expect(sessionA.flags).toEqual({
    example_outbound_enabled: true,
    new_settings_ui: true,
  });
});

test('F5 only a platform operator reaches the routes', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const before = await listFlags(operator);
  const admin = tenantA.admin;
  const forbidden = 'PLATFORM_OPERATOR_REQUIRED';

  await expectError(await admin.request.get(FLAGS), 403, forbidden);
  await expectError(await mutate(admin, 'PUT', `${FLAGS}/${RELEASE_KEY}`, { enabled: true }), 403, forbidden);
  await expectError(await mutate(admin, 'DELETE', `${FLAGS}/${RELEASE_KEY}`), 403, forbidden);
  await expectError(
    await mutate(admin, 'PUT', tenantPath(RELEASE_KEY, tenantA.id), {
      enabled: true,
    }),
    403,
    forbidden,
  );
  await expectError(await mutate(admin, 'DELETE', tenantPath(RELEASE_KEY, tenantA.id)), 403, forbidden);

  expect(await listFlags(operator)).toEqual(before);
});

test('F6t an undeclared key is not defined in the manifest on a tenant path', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  await expectError(
    await mutate(operator, 'PUT', tenantPath('flag_ghost', tenantA.id), {
      enabled: true,
    }),
    404,
    'FLAG_NOT_DEFINED_IN_MANIFEST',
  );
});

test('F7t the tenant body must carry a boolean enabled', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const path = tenantPath(RELEASE_KEY, tenantA.id);
  const wrongType = await expectError(await mutate(operator, 'PUT', path, { enabled: 'yes' }), 422, 'VALIDATION_FAILED');
  expect(wrongType.issues).toEqual([expect.objectContaining({ field: 'enabled', code: 'FIELD_TYPE_INVALID' })]);

  const missing = await expectError(await mutate(operator, 'PUT', path, {}), 422, 'VALIDATION_FAILED');
  expect(missing.issues).toEqual([expect.objectContaining({ field: 'enabled', code: 'FIELD_REQUIRED' })]);
});

test('F9t the operator listing shows every tenant override', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const page = await listFlags(operator);
  const ops = page.items.find((item) => item.key === OPS_KEY);
  const release = page.items.find((item) => item.key === RELEASE_KEY);

  expect(release?.tenantOverrides).toEqual(expect.arrayContaining([expect.objectContaining({ tenantId: tenantA.id, enabled: true })]));
  expect(ops?.tenantOverrides).toEqual(expect.arrayContaining([expect.objectContaining({ tenantId: tenantB.id, enabled: false })]));
});

test('F10 clearing a tenant override falls back', { tag: ['@feature-flags', '@tenancy'] }, async () => {
  const path = tenantPath(RELEASE_KEY, tenantA.id);
  await expectData<FlagView>(await mutate(operator, 'DELETE', path), 200);
  await expectData<FlagView>(await mutate(operator, 'DELETE', path), 200);

  expect((await readFlags(tenantA.admin))[RELEASE_KEY]).toBe(false);
});
