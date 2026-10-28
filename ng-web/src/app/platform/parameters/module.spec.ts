import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { coreModule, platformRoutes, meetsRequirement } from '@/platform/core';
import type { SessionView } from '@/platform/core';
import { CONFIG_URL, SESSION_URL, nextCall, stubBroadcastChannel } from '../../../testing/fixtures/session-testing';
import { okEnvelope, sessionView } from '../../../testing/fixtures/session-view';
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

    const member = sessionView() as unknown as SessionView;

    const operatorWithoutTenant = sessionView({ account: { ...(member.account as object), role: null, isPlatformOperator: true }, activeTenant: null }) as unknown as SessionView;

    const userWithoutTenant = sessionView({ account: { ...(member.account as object), role: null }, activeTenant: null }) as unknown as SessionView;

    expect(meetsRequirement(member, entry.requires)).toBe(true);
    expect(meetsRequirement(operatorWithoutTenant, entry.requires)).toBe(true);
    expect(meetsRequirement(userWithoutTenant, entry.requires)).toBe(false);
  });

  it('loads the list screen on the empty child path', async () => {
    const loaded = await parametersRoutes[0].loadComponent?.();

    expect(loaded).toBe(ParameterListPage);
  });

  describe('route guard', () => {
    let http: HttpTestingController;

    let router: Router;

    beforeEach(() => {
      stubBroadcastChannel();

      const [shellRoute] = parametersModule.shellRoutes;

      configureFeatureTest(
        [
          { path: 'parameters', canMatch: shellRoute.canMatch, component: BlankComponent },
          { path: 'no-tenant', component: BlankComponent },
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
      const navigation = router.navigateByUrl('/parameters');

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(session));
      await navigation;

      return router.url;
    }

    it('opens for a member with a tenant', async () => {
      expect(await visit(sessionView())).toBe('/parameters');
    });

    it('opens for a platform operator without a tenant', async () => {
      const account = { ...(sessionView()['account'] as object), role: null, isPlatformOperator: true };

      expect(await visit(sessionView({ account, activeTenant: null }))).toBe('/parameters');
    });

    it('keeps out an account with no tenant that is not a platform operator', async () => {
      const account = { ...(sessionView()['account'] as object), role: null };

      expect(await visit(sessionView({ account, activeTenant: null }))).toBe('/no-tenant');
    });
  });
});
