import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, Routes } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { coreModule, platformRoutes, providePlatform, requireAccess } from '@/platform/core';
import { errorEnvelope, okEnvelope } from '../../../testing/fixtures/api-envelope';
import { CONFIG_URL, stubBroadcastChannel } from '../../../testing/fixtures/http-testing';
import { identityModule } from './module';
import { sessionView } from './testing/session-view';

const SESSION_URL = '/api/v1/auth/session';

const MODULES = [coreModule, identityModule];

/** The shell as the skeleton mounts it, reduced to what identity decides: public routes, then the guarded group. */
function stubLayout(): Routes {
  const platform = platformRoutes(MODULES);

  return [...platform.public, { path: 'error-page', component: BlankComponent }, { path: '', canMatch: [requireAccess({})], children: [{ path: 'home', component: BlankComponent }] }];
}

describe('identity routing', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    stubBroadcastChannel();
    configureFeatureTest(stubLayout(), [{ provide: MessageService, useValue: { add: vi.fn() } }]);
    TestBed.configureTestingModule({ providers: [providePlatform(MODULES)] });
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  /** Navigates to `url`, answering the config request and then the session request with `answer`. */
  async function goTo(url: string, answer: { readonly body: object; readonly status?: number }): Promise<string> {
    const navigation = router.navigateByUrl(url);

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    await vi.waitFor(() => http.expectOne(SESSION_URL).flush(answer.body, { status: answer.status ?? 200, statusText: 'Answer' }));
    await navigation;

    return router.url;
  }

  it('sends an anonymous visitor from /home to /login with the page asked for', async () => {
    expect(await goTo('/home', { body: errorEnvelope('SESSION_INVALID'), status: 401 })).toBe('/login?returnUrl=%2Fhome');
  });

  it('sends a signed-in visitor from /login to /home', async () => {
    expect(await goTo('/login', { body: okEnvelope(sessionView()) })).toBe('/home');
  });

  it('opens /login for an anonymous visitor', async () => {
    expect(await goTo('/login', { body: errorEnvelope('SESSION_INVALID'), status: 401 })).toBe('/login');
  });

  it('opens /home for a signed-in visitor', async () => {
    expect(await goTo('/home', { body: okEnvelope(sessionView()) })).toBe('/home');
  });

  it('sends the visitor to the error page when the session cannot be loaded', async () => {
    expect(await goTo('/home', { body: errorEnvelope('INTERNAL_ERROR'), status: 500 })).toBe('/error-page');
  });
});
