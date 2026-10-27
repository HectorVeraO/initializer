import { Location } from '@angular/common';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { ConfirmationService, MessageService } from '@openng/optimus-ui/api';
import { provideOptimus } from '@openng/optimus-ui/config';
import es from '../../../../assets/i18n/es.json';
import { provideApiHttpClient } from '@/core/interceptors/api-http.providers';
import { BlankComponent } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { gatbaseSessionInterceptor } from '../session/session.interceptor';
import { AcceptInvitationPage } from './accept-invitation-page.component';

const TOKEN = 'synthetic-token-value';

const UNAUTHORIZED = { status: 401, statusText: 'Unauthorized' };

/** The invitation token leaves the router's URL, not only the address bar, so later navigations cannot copy it back. */
describe('AcceptInvitationPage router URL', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    stubBroadcastChannel();
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [
        provideApiHttpClient([gatbaseSessionInterceptor]),
        provideHttpClientTesting(),
        provideRouter([
          { path: 'home', component: BlankComponent },
          { path: 'login', component: BlankComponent },
          { path: 'invitations/accept', component: AcceptInvitationPage }
        ]),
        provideOptimus({}),
        ConfirmationService,
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    history.replaceState(null, '', '/');
    http.verify();
    vi.unstubAllGlobals();
  });

  it('never puts the token back in the address bar when an expired session sends the person to /login', async () => {
    await loadSession(sessionView());

    const router = TestBed.inject(Router);

    const location = TestBed.inject(Location);

    const harness = await RouterTestingHarness.create();

    // TestBed's Location is a mock; the page reads the fragment from the document, so the document carries the same URL as a real browser would.
    history.replaceState(null, '', `/invitations/accept#token=${TOKEN}`);

    await harness.navigateByUrl(`/invitations/accept#token=${TOKEN}`);
    harness.detectChanges();

    expect(location.path(true)).toBe('/invitations/accept');
    await vi.waitFor(() => expect(router.url).toBe('/invitations/accept'));

    const submit = await vi.waitFor(() => {
      const button = document.querySelector<HTMLButtonElement>('[data-testid="accept-submit"]');

      expect(button).not.toBeNull();

      return button as HTMLButtonElement;
    });

    submit.click();

    // The session expired: the accept call answers 401 SESSION_INVALID and the refresh says the person is anonymous.
    (await nextCall('POST', '/api/v1/invitations/accept')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
    (await nextCall('POST', '/api/v1/auth/refresh')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);

    await vi.waitFor(() => expect(location.path(true)).toContain('/login'));

    expect(router.url).not.toContain(TOKEN);
    expect(location.path(true)).not.toContain(TOKEN);
  });
});
