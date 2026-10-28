import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { NO_FLAGS, coreModule, firstUnmetClause, platformRoutes } from '@/platform/core';
import type { AccessContext } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../testing/fixtures/access-testing';
import { ApiKeysPage } from './api-keys-page.component';
import { apiKeysModule } from './module';

const TENANTLESS_OPERATOR = accessContext({ activeTenant: null, role: null, isPlatformOperator: true, memberships: [] });

const TENANTLESS_MEMBER = accessContext({ activeTenant: null, role: null, memberships: [] });

describe('apiKeysModule', () => {
  it('mounts one shell route at /api-keys and no public route', () => {
    const routes = platformRoutes([coreModule, apiKeysModule]);

    expect(routes.shell.map((route) => route.path)).toEqual(['api-keys']);
    expect(routes.public).toEqual([]);
  });

  it('loads the keys page', async () => {
    const loaded = await apiKeysModule.shellRoutes[0].loadComponent?.();

    expect(loaded).toBe(ApiKeysPage);
  });

  it('adds one menu entry with the same requirement as the route', () => {
    expect(apiKeysModule.nav.map((entry) => `${entry.id}:${entry.path}`)).toEqual(['api-keys:/api-keys']);

    const [entry] = apiKeysModule.nav;

    expect(firstUnmetClause(accessContext(), NO_FLAGS, true, entry.requires)).toBeNull();
    expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, false, entry.requires)).toBeNull();
    expect(firstUnmetClause(accessContext({ role: 'member' }), NO_FLAGS, true, entry.requires)).toBe('roles');
  });

  describe('route guard', () => {
    let router: Router;

    /** Navigates to /api-keys as `context`; `noTenantPath` null is a console without tenancy. */
    async function visit(context: AccessContext | null, noTenantPath: string | null): Promise<string> {
      const [shellRoute] = apiKeysModule.shellRoutes;

      configureFeatureTest(
        [
          { path: 'api-keys', canMatch: shellRoute.canMatch, component: BlankComponent },
          { path: 'no-tenant', component: BlankComponent },
          { path: 'forbidden', component: BlankComponent },
          { path: 'login', component: BlankComponent }
        ],
        [...provideFakeAccess(context), provideTenantSelection(noTenantPath), { provide: MessageService, useValue: { add: vi.fn() } }]
      );
      router = TestBed.inject(Router);
      await router.navigateByUrl('/api-keys');

      return router.url;
    }

    it('opens for a platform operator in a console without tenancy: the keys are global', async () => {
      expect(await visit(TENANTLESS_OPERATOR, null)).toBe('/api-keys');
    });

    it('sends a non-operator of a console without tenancy to /forbidden', async () => {
      expect(await visit(TENANTLESS_MEMBER, null)).toBe('/forbidden');
    });

    it('opens for a tenant admin with an active tenant', async () => {
      expect(await visit(accessContext(), '/no-tenant')).toBe('/api-keys');
    });

    it('opens for a platform operator without an active tenant', async () => {
      expect(await visit(TENANTLESS_OPERATOR, '/no-tenant')).toBe('/api-keys');
    });

    it('sends a platform operator with an active tenant and the member role to /forbidden', async () => {
      expect(await visit(accessContext({ role: 'member', isPlatformOperator: true }), '/no-tenant')).toBe('/forbidden');
    });

    it('sends a member without an active tenant to the no-tenant page', async () => {
      expect(await visit(TENANTLESS_MEMBER, '/no-tenant')).toBe('/no-tenant');
    });

    it('sends a viewer of the active tenant to /forbidden', async () => {
      expect(await visit(accessContext({ role: 'viewer' }), '/no-tenant')).toBe('/forbidden');
    });

    it('sends an anonymous visitor to sign in', async () => {
      expect(await visit(null, '/no-tenant')).toBe('/login');
    });
  });
});
