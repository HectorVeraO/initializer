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
import { accessContext, provideFakeAccess } from '../../../../testing/fixtures/access-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { nextCall } from '../../../../testing/fixtures/http-testing';
import { AcceptInvitationPage } from './accept-invitation-page.component';

const TOKEN = 'synthetic-token-value';

/** The invitation token leaves the router's URL, not only the address bar, so later navigations cannot copy it back. */
describe('AcceptInvitationPage router URL', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [
        provideApiHttpClient([]),
        provideHttpClientTesting(),
        provideRouter([
          { path: 'home', component: BlankComponent },
          { path: 'login', component: BlankComponent },
          { path: 'invitations/accept', component: AcceptInvitationPage }
        ]),
        provideOptimus({}),
        ConfirmationService,
        { provide: MessageService, useValue: { add: vi.fn() } },
        ...provideFakeAccess(accessContext())
      ]
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    history.replaceState(null, '', '/');
    http.verify();
  });

  it('never puts the token back in the address bar or the router URL, not even after a refused accept', async () => {
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

    (await nextCall('POST', '/api/v1/invitations/accept')).flush(errorEnvelope('INVITATION_INVALID'), { status: 404, statusText: 'Not Found' });

    await vi.waitFor(() => expect(document.querySelector('[data-testid="accept-error"]')).not.toBeNull());

    expect(router.url).not.toContain(TOKEN);
    expect(location.path(true)).not.toContain(TOKEN);
  });
});
