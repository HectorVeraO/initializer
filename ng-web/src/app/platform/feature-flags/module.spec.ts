import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { NO_FLAGS, coreModule, firstUnmetClause, platformRoutes } from '@/platform/core';
import type { AccessContext } from '@/platform/core';
import { accessContext, provideFakeAccess } from '../../../testing/fixtures/access-testing';
import { featureFlagsRoutes } from './feature-flags.routes';
import { FlagDetailPage } from './flag-detail/flag-detail.component';
import { FlagListPage } from './flag-list/flag-list.component';
import { featureFlagsModule } from './module';

function operatorContext(): AccessContext {
  return accessContext({ isPlatformOperator: true });
}

describe('featureFlagsModule', () => {
  it('mounts one shell route at /operator/feature-flags and no public route', () => {
    const routes = platformRoutes([coreModule, featureFlagsModule]);

    expect(routes.shell.map((route) => route.path)).toContain('operator/feature-flags');
    expect(routes.public.map((route) => route.path)).not.toContain('operator/feature-flags');
    expect(featureFlagsModule.publicRoutes).toEqual([]);
    expect(featureFlagsModule.shellRoutes).toHaveLength(1);
  });

  it('adds one menu entry that only a platform operator meets', () => {
    expect(featureFlagsModule.nav).toHaveLength(1);

    const [entry] = featureFlagsModule.nav;

    expect(entry.path).toBe('/operator/feature-flags');
    expect(firstUnmetClause(operatorContext(), NO_FLAGS, true, entry.requires)).toBeNull();
    expect(firstUnmetClause(accessContext(), NO_FLAGS, true, entry.requires)).toBe('platformOperator');
  });

  it('loads the list on the empty path and the detail on :key', async () => {
    const listRoute = featureFlagsRoutes.find((route) => route.path === '');

    const detailRoute = featureFlagsRoutes.find((route) => route.path === ':key');

    expect(await listRoute?.loadComponent?.()).toBe(FlagListPage);
    expect(await detailRoute?.loadComponent?.()).toBe(FlagDetailPage);
  });

  describe('route guard', () => {
    let router: Router;

    async function visit(context: AccessContext | null): Promise<string> {
      const [shellRoute] = featureFlagsModule.shellRoutes;

      configureFeatureTest(
        [
          { path: 'operator/feature-flags', canMatch: shellRoute.canMatch, component: BlankComponent },
          { path: 'forbidden', component: BlankComponent },
          { path: 'login', component: BlankComponent },
          { path: '**', component: BlankComponent }
        ],
        [...provideFakeAccess(context), { provide: MessageService, useValue: { add: vi.fn() } }]
      );
      router = TestBed.inject(Router);
      await router.navigateByUrl('/operator/feature-flags');

      return router.url;
    }

    it('opens for a platform operator', async () => {
      expect(await visit(operatorContext())).toBe('/operator/feature-flags');
    });

    it('sends an admin who is not a platform operator to /forbidden', async () => {
      expect(await visit(accessContext())).toBe('/forbidden');
    });

    it('sends an anonymous visitor to the sign-in redirect', async () => {
      expect(await visit(null)).toBe('/login');
    });
  });
});
