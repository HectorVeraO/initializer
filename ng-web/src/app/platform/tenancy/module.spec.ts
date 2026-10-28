import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { TENANT_DIRECTORY, TENANT_SELECTION, coreModule, platformRoutes, providePlatform } from '@/platform/core';
import { tenancyModule } from './module';

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

  it('binds the tenant directory to the tenants route and the tenant selection to /no-tenant', () => {
    configureFeatureTest();
    TestBed.configureTestingModule({ providers: [providePlatform([coreModule, tenancyModule])] });

    TestBed.inject(TENANT_DIRECTORY)?.list().subscribe();

    const request = TestBed.inject(HttpTestingController).expectOne((candidate) => candidate.url.endsWith('/api/v1/tenants'));

    expect(request.request.method).toBe('GET');
    expect(TestBed.inject(TENANT_SELECTION)?.noTenantPath).toBe('/no-tenant');
  });
});
