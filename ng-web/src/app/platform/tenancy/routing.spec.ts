import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { coreModule, platformRoutes, requireAccess } from '@/platform/core';
import type { AccessContext } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../testing/fixtures/access-testing';
import { tenancyModule } from './module';

const MEMBER_WITHOUT_TENANT = accessContext({ activeTenant: null, role: null, memberships: [] });

/** The shell as the skeleton mounts it, reduced to what tenancy decides. */
function stubLayout(): Routes {
  const platform = platformRoutes([coreModule, tenancyModule]);

  return [
    ...platform.public.map((route) => ({ path: route.path, component: BlankComponent })),
    { path: 'forbidden', component: BlankComponent },
    { path: 'login', component: BlankComponent },
    { path: '', canMatch: [requireAccess({})], children: platform.shell }
  ];
}

describe('tenancy routing', () => {
  async function visit(url: string, context: AccessContext | null): Promise<string> {
    configureFeatureTest(stubLayout(), [...provideFakeAccess(context), provideTenantSelection('/no-tenant'), { provide: MessageService, useValue: { add: vi.fn() } }]);

    const router = TestBed.inject(Router);

    await router.navigateByUrl(url);

    return router.url;
  }

  it('sends a member to /forbidden on /invitations', async () => {
    expect(await visit('/invitations', accessContext({ role: 'member' }))).toBe('/forbidden');
  });

  it('opens /invitations for an admin of the active tenant', async () => {
    expect(await visit('/invitations', accessContext())).toBe('/invitations');
  });

  it('sends a member without an active tenant to /no-tenant on /memberships', async () => {
    expect(await visit('/memberships', MEMBER_WITHOUT_TENANT)).toBe('/no-tenant');
  });

  it('opens /memberships for a member with an active tenant', async () => {
    expect(await visit('/memberships', accessContext({ role: 'member' }))).toBe('/memberships');
  });

  it('opens /no-tenant for any signed-in visitor', async () => {
    expect(await visit('/no-tenant', MEMBER_WITHOUT_TENANT)).toBe('/no-tenant');
  });

  it('keeps /operator/tenants from a visitor who is not a platform operator', async () => {
    expect(await visit('/operator/tenants', accessContext())).toBe('/forbidden');
  });

  it('opens /operator/tenants for a platform operator', async () => {
    expect(await visit('/operator/tenants', accessContext({ isPlatformOperator: true }))).toBe('/operator/tenants');
  });

  it('sends an anonymous visitor on a guarded route to sign in', async () => {
    expect(await visit('/memberships', null)).toBe('/login');
  });

  it('mounts the accept page outside the guarded group: an anonymous visitor reaches it', async () => {
    expect(await visit('/invitations/accept', null)).toBe('/invitations/accept');
  });
});
