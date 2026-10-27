import { HttpTestingController } from '@angular/common/http/testing';
import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { coreModule } from '@/gatbase/core';
import { okEnvelope, sessionView } from './testing/fixtures/session-view';
import { buildAppRoutes } from './app.routes';

@Component({ selector: 'p-blank', template: '' })
class BlankComponent {}

const CONFIG_URL = '/assets/config/runtime-config.json';

const DEV_STUB: Routes = [{ path: 'probe', component: BlankComponent }];

describe('app routes', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    vi.stubGlobal('BroadcastChannel', undefined);
    configureFeatureTest(buildAppRoutes(DEV_STUB, [coreModule]), [{ provide: MessageService, useValue: { add: vi.fn() } }]);
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
});
