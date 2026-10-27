import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, byTestId, configureFeatureTest, renderRoute, settle, textOf, typeInto } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { CONFIG_URL, SESSION_URL, fakeLocation, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { SessionStore } from '../session/session-store.service';
import { LoginPage } from './login-page.component';

const FLOW_URL = '/kratos/self-service/login/browser';

const ACTION = 'http://localhost/kratos/self-service/login?flow=synthetic-flow';

const FLOW = { id: 'synthetic-flow', ui: { action: ACTION, method: 'POST', nodes: [{ attributes: { name: 'csrf_token', type: 'hidden', value: 'synthetic-csrf' }, messages: [] }], messages: [] } };

const BAD_REQUEST = { status: 400, statusText: 'Bad Request' };

describe('LoginPage', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    stubBroadcastChannel();
    configureFeatureTest(
      [
        { path: 'login', component: LoginPage },
        { path: 'home', component: BlankComponent },
        { path: 'memberships', component: BlankComponent }
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
    history.replaceState(null, '');
    http.verify();
    vi.unstubAllGlobals();
  });

  async function openLogin(query = ''): Promise<ComponentFixture<LoginPage>> {
    const rendering = renderRoute(`/login${query}`, LoginPage);

    // The first translation or request that needs the config asks for it; answer it once, whoever asks.
    const fixture = await rendering;

    await settle(fixture);

    return fixture;
  }

  /** Fills the form and presses Sign in; answers the runtime config the first sign-in asks for. */
  async function submit(fixture: ComponentFixture<LoginPage>, email = 'user@example.com', password = 'synthetic-password'): Promise<void> {
    const root = fixture.nativeElement as HTMLElement;

    typeInto(byTestId(root, 'login-email') as HTMLInputElement, email);
    typeInto(byTestId(root, 'login-password') as HTMLInputElement, password);
    await settle(fixture);
    (byTestId(root, 'login-submit') as HTMLButtonElement).click();
    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
  }

  async function answerFlow(): Promise<void> {
    (await nextCall('GET', FLOW_URL)).flush(FLOW);
  }

  async function answerPost(body: object = {}, options: object = { status: 200, statusText: 'OK' }): Promise<void> {
    (await nextCall('POST', ACTION)).flush(body, options);
  }

  async function answerSession(view: object | 'locked'): Promise<void> {
    const request = await nextCall('GET', SESSION_URL);

    if (view === 'locked') {
      request.flush(errorEnvelope('ACCOUNT_LOCKED_OR_DISABLED'), { status: 403, statusText: 'Forbidden' });
    } else {
      request.flush(okEnvelope(view));
    }
  }

  async function finish(fixture: ComponentFixture<LoginPage>): Promise<void> {
    await vi.waitFor(() => expect(fixture.componentInstance).toBeTruthy());
    await settle(fixture);
  }

  it('sends the typed credentials to Kratos and lands on the return URL', async () => {
    const fixture = await openLogin('?returnUrl=%2Fmemberships');

    await submit(fixture, 'jane@example.com', 'synthetic-password');
    await answerFlow();

    const post = await nextCall('POST', ACTION);

    expect(post.request.body).toEqual({ method: 'password', identifier: 'jane@example.com', password: 'synthetic-password', csrf_token: 'synthetic-csrf' });
    post.flush({});
    await answerSession(sessionView());
    await vi.waitFor(() => expect(router.url).toBe('/memberships'));
  });

  it('lands on /home without a return URL', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost();
    await answerSession(sessionView());
    await vi.waitFor(() => expect(router.url).toBe('/home'));
  });

  it.each(['//evil.example.com', '/\\evil.example.com', 'https://evil.example.com', 'javascript:alert(1)'])('ignores the unsafe return URL %s', async (returnUrl) => {
    const fixture = await openLogin(`?returnUrl=${encodeURIComponent(returnUrl)}`);

    await submit(fixture);
    await answerFlow();
    await answerPost();
    await answerSession(sessionView());
    await vi.waitFor(() => expect(router.url).toBe('/home'));
  });

  it('treats session_already_available on the POST as signed in', async () => {
    const fixture = await openLogin('?returnUrl=%2Fmemberships');

    await submit(fixture);
    await answerFlow();
    await answerPost({ error: { id: 'session_already_available' } }, BAD_REQUEST);
    await answerSession(sessionView());
    await vi.waitFor(() => expect(router.url).toBe('/memberships'));
  });

  it('prefills the email and shows the notice from the history state', async () => {
    history.replaceState({ email: 'invited@example.com', noticeKey: 'invitationAccepted' }, '');

    const fixture = await openLogin();

    const root = fixture.nativeElement as HTMLElement;

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(root, 'login-notice'))).toBe('Tu invitación fue aceptada. Inicia sesión para continuar.');
    });
    expect((byTestId(root, 'login-email') as HTMLInputElement).value).toBe('invited@example.com');
  });

  it('shows the mapped text for invalid credentials', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost({ ui: { messages: [{ id: 4000006, text: 'The provided credentials are invalid.', type: 'error' }], nodes: [] } }, BAD_REQUEST);
    await finish(fixture);

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toBe('El correo o la contraseña no son válidos.');
    });
    expect(router.url).toBe('/login');
  });

  it('shows an unmapped Kratos message as plain text, never as markup', async () => {
    const fixture = await openLogin();

    const hostile = '<img src=x onerror=alert(1)>';

    await submit(fixture);
    await answerFlow();
    await answerPost({ ui: { messages: [{ id: 4999999, text: hostile, type: 'error' }], nodes: [] } }, BAD_REQUEST);

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toBe(hostile);
    });
    expect(fixture.nativeElement.querySelector('[data-testid="login-error"] img')).toBeNull();
  });

  it('says sign in is unavailable when Kratos cannot be reached', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    (await nextCall('GET', FLOW_URL)).flush({}, { status: 503, statusText: 'Unavailable' });

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toContain('no está disponible');
    });
  });

  it('shows the locked-account text when Kratos accepts but the backend refuses the account', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost();
    await answerSession('locked');

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toContain('bloqueada o deshabilitada');
    });
    expect(router.url).toBe('/login');
  });

  it('falls back to the generic rejection text when Kratos sends no message', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost({}, BAD_REQUEST);

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toBe('El correo o la contraseña no son válidos.');
    });
  });

  it('says sign in is unavailable when the session cannot be reloaded after Kratos accepts', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost();
    (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toContain('no está disponible');
    });
    expect(router.url).toBe('/login');
  });

  it('does not claim a locked account when the session stays anonymous without a denial', async () => {
    const fixture = await openLogin();

    await submit(fixture);
    await answerFlow();
    await answerPost();
    (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).toContain('no está disponible');
    });
    expect(textOf(byTestId(fixture.nativeElement, 'login-error'))).not.toContain('bloqueada');
  });

  it('shows the denial notice recorded by the session store when the page opens', async () => {
    const loaded = TestBed.inject(SessionStore).ensureLoaded();

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('ACCOUNT_LOCKED_OR_DISABLED'), { status: 403, statusText: 'Forbidden' });
    await loaded.catch(() => undefined);

    const fixture = await openLogin();

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(fixture.nativeElement, 'login-notice'))).toContain('bloqueada o deshabilitada');
    });
  });

  it('disables the button while a submit runs and enables it afterwards', async () => {
    const fixture = await openLogin();

    await submit(fixture);

    const button = byTestId(fixture.nativeElement, 'login-submit') as HTMLButtonElement;

    fixture.detectChanges();
    expect(button.disabled).toBe(true);

    (await nextCall('GET', FLOW_URL)).flush({}, { status: 503, statusText: 'Unavailable' });

    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(button.disabled).toBe(false);
    });
  });
});
