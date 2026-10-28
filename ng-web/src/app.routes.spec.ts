import { HttpTestingController } from '@angular/common/http/testing';
import { Provider } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { WebModule, anonymousGuard, coreModule, providePlatform, requireAccess } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from './testing/fixtures/access-testing';
import { errorEnvelope, okEnvelope } from './testing/fixtures/api-envelope';
import { sessionView } from './testing/fixtures/session-view';
import { buildAppRoutes } from './app.routes';

const CONFIG_URL = '/assets/config/runtime-config.json';

const DEV_STUB: Routes = [{ path: 'probe', component: BlankComponent }];

const IGNORED_I18N = { scope: 'x', alias: 'x', loader: { es: () => Promise.resolve({}) } };

/** A module that contributes nothing: the console without identity. */
const BARE_CORE: WebModule = { id: 'core', shellRoutes: [], publicRoutes: [], nav: [], i18n: IGNORED_I18N };

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
    configure([BARE_CORE]);

    expect(await resolve(url)).toBe(url);
  });

  it('redirects the empty path to /home', async () => {
    configure([BARE_CORE]);

    expect(await resolve('/')).toBe('/home');
  });

  it.each(['/login', '/memberships', '/operator/tenants', '/no-tenant'])('sends %s to /notfound when no module declares it', async (url) => {
    configure([BARE_CORE]);

    expect(await resolveWithoutLayout(url)).toBe('/notfound');
  });

  it('sends an unknown path to /notfound', async () => {
    configure([BARE_CORE]);

    expect(await resolveWithoutLayout('/no-such-page')).toBe('/notfound');
  });

  it.each(['/notfound', '/error-page'])('still resolves %s without any request', async (url) => {
    configure([BARE_CORE]);
    await router.navigateByUrl(url);

    expect(router.url).toBe(url);
    TestBed.inject(HttpTestingController).expectNone(CONFIG_URL);
  });

  it('mounts a multi-segment public route of a module outside the shell', async () => {
    configure([BARE_CORE, { ...BARE_CORE, id: 'invites', publicRoutes: [{ path: 'invitations/accept', component: BlankComponent }] }]);
    await router.navigateByUrl('/invitations/accept');

    expect(router.url).toBe('/invitations/accept');
    TestBed.inject(HttpTestingController).expectNone(CONFIG_URL);
  });

  describe('with a multi-tenant module', () => {
    const MULTI_TENANT: WebModule = {
      ...BARE_CORE,
      id: 'tenants',
      shellRoutes: [
        { path: 'no-tenant', canMatch: [requireAccess({})], component: BlankComponent },
        { path: 'reports', canMatch: [requireAccess({ tenant: 'required' })], component: BlankComponent }
      ]
    };

    it('sends a tenantless visitor on a required-tenant route to the bound no-tenant path', async () => {
      configure([BARE_CORE, MULTI_TENANT], [...provideFakeAccess(accessContext({ activeTenant: null, role: null, memberships: [] })), provideTenantSelection('/no-tenant')]);

      expect(await resolve('/reports')).toBe('/no-tenant');
    });

    it('lets a visitor with an active tenant through', async () => {
      configure([BARE_CORE, MULTI_TENANT], [...provideFakeAccess(accessContext()), provideTenantSelection('/no-tenant')]);

      expect(await resolve('/reports')).toBe('/reports');
    });

    it('sends the same tenantless visitor to /forbidden when the console has no tenant selection', async () => {
      configure([BARE_CORE, MULTI_TENANT], [...provideFakeAccess(accessContext({ activeTenant: null, role: null, memberships: [] })), provideTenantSelection(null)]);

      expect(await resolve('/reports')).toBe('/forbidden');
    });

    it('sends an anonymous visitor to the sign-in redirect of the access provider', async () => {
      configure([BARE_CORE, { ...MULTI_TENANT, publicRoutes: [{ path: 'login', component: BlankComponent }] }], [...provideFakeAccess(null), provideTenantSelection('/no-tenant')]);

      expect(await resolveWithoutLayout('/reports')).toBe('/login');
    });
  });
});

// The identity web code still lives in the core module; its login stub gives the anonymous redirect a target.
const APP_MODULE: WebModule = {
  ...coreModule,
  publicRoutes: [
    { path: 'login', canMatch: [anonymousGuard], component: BlankComponent },
    { path: 'invitations/accept', component: BlankComponent }
  ]
};

describe('app routes with the core module', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    vi.stubGlobal('BroadcastChannel', undefined);
    configureFeatureTest(buildAppRoutes(DEV_STUB, [APP_MODULE]), [{ provide: MessageService, useValue: { add: vi.fn() } }]);
    TestBed.configureTestingModule({ providers: [providePlatform([APP_MODULE])] });
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  /** Navigates to `url`, answering the config and session requests the route guards trigger. */
  async function resolve(url: string): Promise<string> {
    const navigation = router.navigateByUrl(url);

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));

    const session = await vi.waitFor(() => http.expectOne('/api/v1/auth/session'));

    session.flush(okEnvelope(sessionView()));
    await navigation;

    return router.url;
  }

  it.each(['/home', '/forbidden', '/dev/probe'])('still resolves %s in the layout', async (url) => {
    // The layout's config guard is the only thing /forbidden and /dev wait for; /home also waits for the session.
    const navigation = router.navigateByUrl(url);

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));

    if (url === '/home') {
      await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(okEnvelope(sessionView())));
    }

    await navigation;

    expect(router.url).toBe(url);
  });

  it('sends an anonymous visitor from /home to /login', async () => {
    const navigation = router.navigateByUrl('/home');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' }));
    await navigation;

    expect(router.url).toBe('/login?returnUrl=%2Fhome');
  });

  it('redirects the empty path to /home', async () => {
    expect(await resolve('/')).toBe('/home');
  });

  it.each(['/notfound', '/error-page'])('still resolves %s without any request', async (url) => {
    await router.navigateByUrl(url);

    expect(router.url).toBe(url);
    http.expectNone(CONFIG_URL);
  });

  it('sends an unknown path to /notfound', async () => {
    expect(await resolve('/no-such-page')).toBe('/notfound');
  });

  it('keeps /invitations in the guarded shell: an anonymous visitor is sent to /login', async () => {
    const navigation = router.navigateByUrl('/invitations');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' }));
    await navigation;

    expect(router.url).toBe('/login?returnUrl=%2Finvitations');
  });

  it('sends a signed-in member without the admin role from /invitations to /forbidden', async () => {
    const member = { id: 'a', email: 'member@example.com', fullName: null, role: 'member', isPlatformOperator: false };

    const navigation = router.navigateByUrl('/invitations');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(okEnvelope(sessionView({ account: member }))));
    await navigation;

    expect(router.url).toBe('/forbidden');
  });

  it('sends a signed-in visitor without a tenant from /memberships to /no-tenant', async () => {
    const navigation = router.navigateByUrl('/memberships');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(okEnvelope(sessionView({ activeTenant: null, memberships: [] }))));
    await navigation;

    expect(router.url).toBe('/no-tenant');
  });

  it('mounts a multi-segment public route outside the shell', async () => {
    await router.navigateByUrl('/invitations/accept');

    expect(router.url).toBe('/invitations/accept');
    http.expectNone('/api/v1/auth/session');
  });
});
