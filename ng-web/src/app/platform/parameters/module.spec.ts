import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import type { AccessContext } from '@/platform/core';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { NO_FLAGS, coreModule, firstUnmetClause, platformRoutes } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../testing/fixtures/access-testing';
import { parametersModule } from './module';
import { ParameterListPage } from './parameter-list/parameter-list.component';
import { parametersRoutes } from './parameters.routes';

describe('parametersModule', () => {
  it('mounts one shell route at /parameters and no public route', () => {
    const routes = platformRoutes([coreModule, parametersModule]);

    expect(routes.shell.map((route) => route.path)).toContain('parameters');
    expect(routes.public.map((route) => route.path)).not.toContain('parameters');
    expect(parametersModule.publicRoutes).toEqual([]);
    expect(parametersModule.shellRoutes).toHaveLength(1);
  });

  it('adds one menu entry that needs a tenant unless the account is a platform operator', () => {
    expect(parametersModule.nav).toHaveLength(1);

    const [entry] = parametersModule.nav;

    expect(entry.path).toBe('/parameters');

    const member = accessContext();

    const operatorWithoutTenant = accessContext({ activeTenant: null, role: null, isPlatformOperator: true, memberships: [] });

    const userWithoutTenant = accessContext({ activeTenant: null, role: null, memberships: [] });

    expect(firstUnmetClause(member, NO_FLAGS, true, entry.requires)).toBeNull();
    expect(firstUnmetClause(operatorWithoutTenant, NO_FLAGS, true, entry.requires)).toBeNull();
    expect(firstUnmetClause(userWithoutTenant, NO_FLAGS, true, entry.requires)).toBe('tenant');
  });

  it('loads the list screen on the empty child path', async () => {
    const loaded = await parametersRoutes[0].loadComponent?.();

    expect(loaded).toBe(ParameterListPage);
  });

  describe('route guard', () => {
    let router: Router;

    function configure(context: AccessContext | null): void {
      const [shellRoute] = parametersModule.shellRoutes;

      configureFeatureTest(
        [
          { path: 'parameters', canMatch: shellRoute.canMatch, component: BlankComponent },
          { path: 'no-tenant', component: BlankComponent },
          { path: 'forbidden', component: BlankComponent },
          { path: 'login', component: BlankComponent },
          { path: '**', component: BlankComponent }
        ],
        [...provideFakeAccess(context), provideTenantSelection('/no-tenant'), { provide: MessageService, useValue: { add: vi.fn() } }]
      );
      router = TestBed.inject(Router);
    }

    async function visit(context: AccessContext | null): Promise<string> {
      configure(context);
      await router.navigateByUrl('/parameters');

      return router.url;
    }

    it('opens for a member with a tenant', async () => {
      expect(await visit(accessContext())).toBe('/parameters');
    });

    it('opens for a platform operator without a tenant', async () => {
      expect(await visit(accessContext({ activeTenant: null, role: null, isPlatformOperator: true, memberships: [] }))).toBe('/parameters');
    });

    it('keeps out an account with no tenant that is not a platform operator', async () => {
      expect(await visit(accessContext({ activeTenant: null, role: null, memberships: [] }))).toBe('/no-tenant');
    });

    it('sends an anonymous visitor to the sign-in redirect', async () => {
      expect(await visit(null)).toBe('/login');
    });
  });
});
