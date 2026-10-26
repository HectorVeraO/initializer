import { expect, test } from '@playwright/test';
import { bootstrapOperator, expectError, mutate, seedTenant } from '../../tck-support/actors';

test('CA-12 tenant creation refuses a taken slug and accumulates validation issues', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'tenant-a' });

  const taken = await mutate(operator, 'POST', '/api/v1/tenants', {
    slug: tenant.slug,
    name: 'Tenant A again',
    adminEmail: `taken-${tenant.slug}@example.com`,
  });
  await expectError(taken, 409, 'TENANT_SLUG_TAKEN');

  const invalid = await mutate(operator, 'POST', '/api/v1/tenants', {
    slug: '0b9c4c6e-0000-4000-8000-000000000001',
    name: '',
    adminEmail: 'x',
  });
  const error = await expectError(invalid, 422, 'VALIDATION_FAILED');
  expect(error.issues.map((issue) => [issue.field, issue.code])).toEqual([
    ['slug', 'FIELD_INVALID_FORMAT'],
    ['name', 'FIELD_REQUIRED'],
    ['adminEmail', 'FIELD_INVALID_FORMAT'],
  ]);

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});

test('CA-12 only a platform operator creates tenants', { tag: '@core-auth' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenant = await seedTenant(playwright, operator, { slugBase: 'tenant-ops' });

  const refused = await mutate(tenant.admin, 'POST', '/api/v1/tenants', {
    slug: `refused-${tenant.slug}`,
    name: 'Refused',
    adminEmail: `refused-${tenant.slug}@example.com`,
  });
  await expectError(refused, 403, 'PLATFORM_OPERATOR_REQUIRED');

  await tenant.admin.request.dispose();
  await operator.request.dispose();
});
