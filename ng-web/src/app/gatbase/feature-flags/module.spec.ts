import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { coreModule, gatbaseRoutes, meetsRequirement } from '@/gatbase/core';
import type { SessionView } from '@/gatbase/core';
import { CONFIG_URL, SESSION_URL, nextCall, stubBroadcastChannel } from '../../../testing/fixtures/session-testing';
import { okEnvelope, sessionView } from '../../../testing/fixtures/session-view';
import { featureFlagsRoutes } from './feature-flags.routes';
import { FlagDetailPage } from './flag-detail/flag-detail.component';
import { FlagListPage } from './flag-list/flag-list.component';
import { featureFlagsModule } from './module';

function operatorSession(): Record<string, unknown> {
  const account = { ...(sessionView()['account'] as object), isPlatformOperator: true };

  return sessionView({ account });
}

describe('featureFlagsModule', () => {
  it('mounts one shell route at /operator/feature-flags and no public route', () => {
    const routes = gatbaseRoutes([coreModule, featureFlagsModule]);

    expect(routes.shell.map((route) => route.path)).toContain('operator/feature-flags');
    expect(routes.public.map((route) => route.path)).not.toContain('operator/feature-flags');
    expect(featureFlagsModule.publicRoutes).toEqual([]);
    expect(featureFlagsModule.shellRoutes).toHaveLength(1);
  });

  it('adds one menu entry that only a platform operator meets', () => {
    expect(featureFlagsModule.nav).toHaveLength(1);

    const [entry] = featureFlagsModule.nav;

    expect(entry.path).toBe('/operator/feature-flags');
    expect(meetsRequirement(operatorSession() as unknown as SessionView, entry.requires)).toBe(true);
    expect(meetsRequirement(sessionView() as unknown as SessionView, entry.requires)).toBe(false);
  });

  it('loads the list on the empty path and the detail on :key', async () => {
    const listRoute = featureFlagsRoutes.find((route) => route.path === '');

    const detailRoute = featureFlagsRoutes.find((route) => route.path === ':key');

    expect(await listRoute?.loadComponent?.()).toBe(FlagListPage);
    expect(await detailRoute?.loadComponent?.()).toBe(FlagDetailPage);
  });

  describe('route guard', () => {
    let http: HttpTestingController;

    let router: Router;

    beforeEach(() => {
      stubBroadcastChannel();

      const [shellRoute] = featureFlagsModule.shellRoutes;

      configureFeatureTest(
        [
          { path: 'operator/feature-flags', canMatch: shellRoute.canMatch, component: BlankComponent },
          { path: 'forbidden', component: BlankComponent },
          { path: 'login', component: BlankComponent },
          { path: '**', component: BlankComponent }
        ],
        [{ provide: MessageService, useValue: { add: vi.fn() } }]
      );
      http = TestBed.inject(HttpTestingController);
      router = TestBed.inject(Router);
    });

    afterEach(() => {
      http.verify();
      vi.unstubAllGlobals();
    });

    async function visit(session: object): Promise<string> {
      const navigation = router.navigateByUrl('/operator/feature-flags');

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(session));
      await navigation;

      return router.url;
    }

    it('opens for a platform operator', async () => {
      expect(await visit(operatorSession())).toBe('/operator/feature-flags');
    });

    it('sends an admin who is not a platform operator to /forbidden', async () => {
      expect(await visit(sessionView())).toBe('/forbidden');
    });
  });
});
