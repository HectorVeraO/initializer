import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectData, mutate } from '../../tck-support/actors';
import { bootstrapTenantSlug } from '../../tck-support/actors/tenants';

interface SessionView {
  account: { role: string | null; isPlatformOperator: boolean };
  activeTenant: { id: string; slug: string } | null;
  memberships: Array<{ tenant: { slug: string }; role: string }>;
}

test('CA-01 the Day-0 session of the bootstrap operator is admin of the bootstrap tenant', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const slug = bootstrapTenantSlug();

  const session = await expectData<SessionView>(await operator.request.get('/api/v1/auth/session'), 200);

  expect(session.account.role).toBe('admin');
  expect(session.account.isPlatformOperator).toBe(true);
  expect(session.activeTenant?.slug).toBe(slug);
  expect(session.memberships.some((item) => item.tenant.slug === slug && item.role === 'admin')).toBe(true);
  await operator.request.dispose();
});

test('CA-17 switch-tenant onto the active tenant answers its session view', { tag: '@tenancy' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const slug = bootstrapTenantSlug();
  const session = await expectData<SessionView>(await operator.request.get('/api/v1/auth/session'), 200);
  expect(session.activeTenant?.slug).toBe(slug);

  const switched = await mutate(operator, 'POST', '/api/v1/auth/switch-tenant', {
    tenantId: session.activeTenant?.id,
  });
  const view = await expectData<SessionView>(switched, 200);

  expect(view.activeTenant?.slug).toBe(slug);
  expect(view.activeTenant?.id).toBe(session.activeTenant?.id);
  expect(switched.headers()['cache-control'] ?? '').toContain('no-store');
  await operator.request.dispose();
});
