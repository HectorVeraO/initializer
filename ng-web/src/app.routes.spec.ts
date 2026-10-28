import { HttpTestingController } from '@angular/common/http/testing';
import { Provider } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { AppLayout } from '@/layout/component/layout/app.layout';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { WebModule, coreModule, requireAccess } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from './testing/fixtures/access-testing';
import { buildAppRoutes } from './app.routes';

const CONFIG_URL = '/assets/config/runtime-config.json';

const DEV_STUB: Routes = [{ path: 'probe', component: BlankComponent }];

describe('app routes without identity', () => {
  let router: Router;

  function configure(modules: readonly WebModule[], providers: Provider[] = []): void {
    configureFeatureTest(buildAppRoutes(DEV_STUB, modules), [{ provide: MessageService, useValue: { add: vi.fn() } }, ...providers]);
    router = TestBed.inject(Router);
  }

  /** Navigates to a path the shell group never activates: no runtime config is requested. */
  async function resolveWithoutLayout(url: string): Promise<string> {
    await router.navigateByUrl(url);
    TestBed.inject(HttpTestingController).expectNone(CONFIG_URL);

    return router.url;
  }

  /** Navigates to `url` once the layout's config guard has loaded the runtime config. */
  async function resolve(url: string): Promise<string> {
    const navigation = router.navigateByUrl(url);

    await vi.waitFor(() => TestBed.inject(HttpTestingController).expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await navigation;

    return router.url;
  }

  afterEach(() => {
    TestBed.inject(HttpTestingController).verify();
  });

  it.each(['/home', '/forbidden', '/dev/probe'])('renders %s in the layout for an anonymous visitor', async (url) => {
    configure([coreModule]);

    expect(await resolve(url)).toBe(url);
    expect(router.routerState.snapshot.root.firstChild?.component).toBe(AppLayout);
  });

  it('redirects the empty path to /home', async () => {
    configure([coreModule]);

    expect(await resolve('/')).toBe('/home');
  });

  it.each(['/login', '/memberships', '/operator/tenants', '/no-tenant'])('sends %s to /notfound when no module declares it', async (url) => {
    configure([coreModule]);

    expect(await resolveWithoutLayout(url)).toBe('/notfound');
  });

  it('sends an unknown path to /notfound', async () => {
    configure([coreModule]);

    expect(await resolveWithoutLayout('/no-such-page')).toBe('/notfound');
  });

  it.each(['/notfound', '/error-page'])('still resolves %s without any request', async (url) => {
    configure([coreModule]);
    await router.navigateByUrl(url);

    expect(router.url).toBe(url);
    TestBed.inject(HttpTestingController).expectNone(CONFIG_URL);
  });

  it('mounts a multi-segment public route of a module outside the shell', async () => {
    configure([coreModule, { ...coreModule, id: 'invites', publicRoutes: [{ path: 'invitations/accept', component: BlankComponent }] }]);
    await router.navigateByUrl('/invitations/accept');

    expect(router.url).toBe('/invitations/accept');
    TestBed.inject(HttpTestingController).expectNone(CONFIG_URL);
  });

  describe('with a multi-tenant module', () => {
    const MULTI_TENANT: WebModule = {
      ...coreModule,
      id: 'tenants',
      shellRoutes: [
        { path: 'no-tenant', canMatch: [requireAccess({})], component: BlankComponent },
        { path: 'reports', canMatch: [requireAccess({ tenant: 'required' })], component: BlankComponent }
      ]
    };

    it('sends a tenantless visitor on a required-tenant route to the bound no-tenant path', async () => {
      configure([coreModule, MULTI_TENANT], [...provideFakeAccess(accessContext({ activeTenant: null, role: null, memberships: [] })), provideTenantSelection('/no-tenant')]);

      expect(await resolve('/reports')).toBe('/no-tenant');
    });

    it('lets a visitor with an active tenant through', async () => {
      configure([coreModule, MULTI_TENANT], [...provideFakeAccess(accessContext()), provideTenantSelection('/no-tenant')]);

      expect(await resolve('/reports')).toBe('/reports');
    });

    it('sends the same tenantless visitor to /forbidden when the console has no tenant selection', async () => {
      configure([coreModule, MULTI_TENANT], [...provideFakeAccess(accessContext({ activeTenant: null, role: null, memberships: [] })), provideTenantSelection(null)]);

      expect(await resolve('/reports')).toBe('/forbidden');
    });

    it('sends an anonymous visitor to the sign-in redirect of the access provider', async () => {
      configure([coreModule, { ...MULTI_TENANT, publicRoutes: [{ path: 'login', component: BlankComponent }] }], [...provideFakeAccess(null), provideTenantSelection('/no-tenant')]);

      expect(await resolveWithoutLayout('/reports')).toBe('/login');
    });
  });
});
