import { test } from '@playwright/test';
import { activeTenantOf, bootstrapOperator, expectError, mutate } from '../../tck-support/actors';
import { seedTenant, tenantlessActor } from '../../tck-support/actors/tenants';
import { defineParameter, overridePath, paramKey } from './support';

test('P10 roles gate overrides and only operators define parameters', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p10-a',
    members: [{ label: 'p10-member', role: 'member' }],
  });
  const member = tenantA.members['p10-member'];
  const key = paramKey('export.p10_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });

  await expectError(await mutate(member, 'PUT', overridePath(key), { value: 5 }), 403, 'INSUFFICIENT_ROLE');
  const valid = {
    key: paramKey('custom.admin_attempt'),
    valueType: 'string',
    defaultValue: 'x',
  };
  await expectError(await mutate(tenantA.admin, 'POST', '/api/v1/parameters', valid), 403, 'PLATFORM_OPERATOR_REQUIRED');
  await expectError(await mutate(tenantA.admin, 'DELETE', `/api/v1/parameters/${key}`), 403, 'PLATFORM_OPERATOR_REQUIRED');
});

test('P11 an unknown key is not found', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);

  await expectError(await mutate(operator, 'DELETE', `/api/v1/parameters/${paramKey('missing')}`), 404, 'RESOURCE_NOT_FOUND');
  await operator.request.dispose();
});

test('P11t an override of an unknown key is not found', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantA = await seedTenant(playwright, operator, {
    slugBase: 'p11t-a',
  });

  await expectError(
    await mutate(tenantA.admin, 'PUT', overridePath(paramKey('missing')), {
      value: 1,
    }),
    404,
    'RESOURCE_NOT_FOUND',
  );
  await tenantA.admin.request.dispose();
  await operator.request.dispose();
});

test('P16 an override write without an active tenant answers 403 ACTIVE_TENANT_REQUIRED', { tag: '@parameters' }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  test.skip((await activeTenantOf(operator)) !== null, 'the operator has a tenant: P16t pins this answer with tenancy');
  const key = paramKey('export.p16_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });

  await expectError(await mutate(operator, 'PUT', overridePath(key), { value: 5 }), 403, 'ACTIVE_TENANT_REQUIRED');
  await operator.request.dispose();
});

test('P16t a member without an active tenant cannot write an override', { tag: ['@parameters', '@tenancy'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const tenantless = await tenantlessActor(playwright, operator);
  const key = paramKey('export.p16t_max_rows');
  await defineParameter(operator, {
    key,
    valueType: 'number',
    defaultValue: 50,
    minValue: 1,
    maxValue: 500,
  });

  await expectError(await mutate(tenantless, 'PUT', overridePath(key), { value: 5 }), 403, 'ACTIVE_TENANT_REQUIRED');
  await tenantless.request.dispose();
  await operator.request.dispose();
});
