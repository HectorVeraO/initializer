import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope, okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { SESSION_URL, fakeLocation } from '../../../../testing/fixtures/session-testing';
import { CONFIG_URL, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { anonymousGuard } from './guards';

type SessionAnswer = object | 'anonymous' | 'server-error' | 'config-error';

describe('anonymousGuard', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    stubBroadcastChannel();
    configureFeatureTest(
      [
        { path: 'login', canMatch: [anonymousGuard], component: BlankComponent },
        { path: 'home', component: BlankComponent },
        { path: 'error-page', component: BlankComponent }
      ],
      [
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    );
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  /** Navigates to `/login`, answering the config and session requests the guard triggers, and returns where the router ended. */
  async function goToLogin(answer: SessionAnswer = sessionView()): Promise<string> {
    const navigation = router.navigateByUrl('/login');

    if (answer === 'config-error') {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' }));
    } else {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));

      const request = await nextCall('GET', SESSION_URL);

      if (answer === 'anonymous') {
        request.flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });
      } else if (answer === 'server-error') {
        request.flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
      } else {
        request.flush(okEnvelope(answer));
      }
    }

    await navigation;

    return router.url;
  }

  it('opens /login for an anonymous visitor', async () => {
    expect(await goToLogin('anonymous')).toBe('/login');
  });

  it('sends an authenticated person home', async () => {
    expect(await goToLogin()).toBe('/home');
  });

  it('sends the person to the error page when the session cannot be loaded', async () => {
    expect(await goToLogin('server-error')).toBe('/error-page');
  });

  it('sends the person to the error page when the runtime config failed', async () => {
    expect(await goToLogin('config-error')).toBe('/error-page');
  });
});
