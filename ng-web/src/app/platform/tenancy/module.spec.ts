import { TestBed } from '@angular/core/testing';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { TENANT_DIRECTORY, TENANT_SELECTION, coreModule, platformRoutes, providePlatform } from '@/platform/core';
import { tenancyModule } from './module';
import { TenantsApi } from './tenants/tenants-api.service';

describe('tenancyModule', () => {
  it('mounts the four shell routes and the public accept page', () => {
    const routes = platformRoutes([coreModule, tenancyModule]);

    expect(routes.shell.map((route) => route.path)).toEqual(['no-tenant', 'memberships', 'invitations', 'operator/tenants']);
    expect(routes.public.map((route) => route.path)).toEqual(['invitations/accept']);
  });

  it('adds the memberships, invitations and operator tenants menu entries with today ids, paths and requirements', () => {
    expect(tenancyModule.nav.map((entry) => `${entry.id}:${entry.path}`)).toEqual(['memberships:/memberships', 'invitations:/invitations', 'operator-tenants:/operator/tenants']);
    expect(tenancyModule.nav.map((entry) => entry.requires)).toEqual([{ tenant: 'required' }, { tenant: 'required', roles: ['admin'] }, { platformOperator: true }]);
  });

  it('puts the tenant switcher in the context slot', () => {
    expect((tenancyModule.topbar ?? []).map((item) => `${item.id}:${item.slot}`)).toEqual(['tenant-switcher:context']);
  });

  it('binds the tenant directory and the tenant selection, and nothing else', () => {
    configureFeatureTest();
    TestBed.configureTestingModule({ providers: [providePlatform([coreModule, tenancyModule])] });

    expect(TestBed.inject(TENANT_DIRECTORY)).toBeInstanceOf(TenantsApi);
    expect(TestBed.inject(TENANT_SELECTION)?.noTenantPath).toBe('/no-tenant');
    expect(tenancyModule.ports).toEqual({ tenants: TenantsApi, tenantSelection: expect.any(Function) });
  });
});
