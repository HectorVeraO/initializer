import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { WebModule, anonymousGuard, coreModule } from '@/platform/core';
import { errorEnvelope } from './testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from './testing/fixtures/session-view';
import { buildAppRoutes } from './app.routes';

const CONFIG_URL = '/assets/config/runtime-config.json';

// The core module ships no login page yet; the stub gives the anonymous redirect a target.
const APP_MODULE: WebModule = {
  ...coreModule,
  publicRoutes: [
    { path: 'login', canMatch: [anonymousGuard], component: BlankComponent },
    { path: 'invitations/accept', component: BlankComponent }
  ]
};

const DEV_STUB: Routes = [{ path: 'probe', component: BlankComponent }];

describe('app routes', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    vi.stubGlobal('BroadcastChannel', undefined);
    configureFeatureTest(buildAppRoutes(DEV_STUB, [APP_MODULE]), [{ provide: MessageService, useValue: { add: vi.fn() } }]);
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

  it('mounts a multi-segment public route outside the shell', async () => {
    await router.navigateByUrl('/invitations/accept');

    expect(router.url).toBe('/invitations/accept');
    http.expectNone('/api/v1/auth/session');
  });
});
