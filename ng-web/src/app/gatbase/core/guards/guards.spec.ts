import { HttpTestingController } from '@angular/common/http/testing';
import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { CONFIG_URL, SESSION_URL, fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../testing/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { anonymousGuard, authGuard, flagGuard, requireSession } from './guards';

@Component({ selector: 'p-blank', template: '' })
class BlankComponent {}

const OPERATOR = { id: 'a', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true };

const VIEWER = { id: 'a', email: 'viewer@example.com', fullName: null, role: 'viewer', isPlatformOperator: false };

type SessionAnswer = object | 'anonymous' | 'locked' | 'server-error' | 'config-error' | 'foreign-api-url';

describe('session guards', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    stubBroadcastChannel();
    configureFeatureTest(
      [
        { path: 'login', canMatch: [anonymousGuard], component: BlankComponent },
        { path: 'home', component: BlankComponent },
        { path: 'private/:id', canMatch: [authGuard], component: BlankComponent },
        { path: 'tenant-page', canMatch: [requireSession({ tenant: 'required' })], component: BlankComponent },
        { path: 'operator-or-tenant-page', canMatch: [requireSession({ tenant: 'required-unless-operator' })], component: BlankComponent },
        { path: 'admin-page', canMatch: [requireSession({ roles: ['admin'] })], component: BlankComponent },
        { path: 'operator-page', canMatch: [requireSession({ platformOperator: true })], component: BlankComponent },
        { path: 'flagged-page', canMatch: [requireSession({ flag: 'beta' })], component: BlankComponent },
        { path: 'flag-only-page', canMatch: [flagGuard('beta')], component: BlankComponent },
        { path: 'everything-page', canMatch: [requireSession({ tenant: 'required', roles: ['admin'], platformOperator: true, flag: 'beta' })], component: BlankComponent },
        { path: 'no-tenant', component: BlankComponent },
        { path: 'forbidden', component: BlankComponent },
        { path: 'error-page', component: BlankComponent },
        { path: 'notfound', component: BlankComponent },
        { path: '**', redirectTo: 'notfound' }
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

  /** Navigates to `url`, answering the config and session requests the guards trigger, and returns where the router ended. */
  async function go(url: string, answer: SessionAnswer = sessionView()): Promise<string> {
    const navigation = router.navigateByUrl(url);

    if (answer === 'foreign-api-url') {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: 'https://api.example.com' }));
    } else if (answer === 'config-error') {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' }));
    } else {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));

      const request = await nextCall('GET', SESSION_URL);

      if (answer === 'anonymous') {
        request.flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });
      } else if (answer === 'locked') {
        request.flush(errorEnvelope('ACCOUNT_LOCKED_OR_DISABLED'), { status: 403, statusText: 'Forbidden' });
      } else if (answer === 'server-error') {
        request.flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
      } else {
        request.flush(okEnvelope(answer));
      }
    }

    await navigation;

    return router.url;
  }

  describe('authGuard', () => {
    it('lets an authenticated session through', async () => {
      expect(await go('/private/42')).toBe('/private/42');
    });

    it('sends an anonymous visitor to /login and keeps the URL asked for', async () => {
      expect(await go('/private/42?x=1', 'anonymous')).toBe('/login?returnUrl=%2Fprivate%2F42%3Fx%3D1');
    });

    it('treats a locked account as anonymous', async () => {
      expect(await go('/private/42', 'locked')).toBe('/login?returnUrl=%2Fprivate%2F42');
    });

    it('sends the person to the error page when the runtime config failed', async () => {
      expect(await go('/private/42', 'config-error')).toBe('/error-page');
      http.expectNone(SESSION_URL);
    });

    it('sends the person to the error page when apiUrl is not the same origin', async () => {
      expect(await go('/private/42', 'foreign-api-url')).toBe('/error-page');
      http.expectNone(SESSION_URL);
    });

    it('sends the person to the error page when the session cannot be loaded', async () => {
      expect(await go('/private/42', 'server-error')).toBe('/error-page');
    });
  });

  describe('anonymousGuard', () => {
    it('opens /login for an anonymous visitor', async () => {
      expect(await go('/login', 'anonymous')).toBe('/login');
    });

    it('sends an authenticated person home', async () => {
      expect(await go('/login')).toBe('/home');
    });

    it('sends the person to the error page when the session cannot be loaded', async () => {
      expect(await go('/login', 'server-error')).toBe('/error-page');
    });

    it('sends the person to the error page when the runtime config failed', async () => {
      expect(await go('/login', 'config-error')).toBe('/error-page');
    });
  });

  describe('requireSession', () => {
    it('sends an anonymous visitor to /login with the returnUrl', async () => {
      expect(await go('/admin-page', 'anonymous')).toBe('/login?returnUrl=%2Fadmin-page');
    });

    it('passes a session with no requirement beyond being authenticated', async () => {
      expect(await go('/tenant-page')).toBe('/tenant-page');
    });

    it('sends a session without a tenant to /no-tenant', async () => {
      expect(await go('/tenant-page', sessionView({ activeTenant: null }))).toBe('/no-tenant');
    });

    it('lets an operator without a tenant through when the tenant is required unless operator', async () => {
      expect(await go('/operator-or-tenant-page', sessionView({ activeTenant: null, account: OPERATOR }))).toBe('/operator-or-tenant-page');
    });

    it('sends a non-operator without a tenant to /no-tenant when the tenant is required unless operator', async () => {
      expect(await go('/operator-or-tenant-page', sessionView({ activeTenant: null, account: VIEWER }))).toBe('/no-tenant');
    });

    it('sends a role outside the allowed ones to /forbidden', async () => {
      expect(await go('/admin-page', sessionView({ account: VIEWER }))).toBe('/forbidden');
    });

    it('lets the allowed role through', async () => {
      expect(await go('/admin-page')).toBe('/admin-page');
    });

    it('sends a session without a role to /forbidden when roles are required', async () => {
      expect(await go('/admin-page', sessionView({ activeTenant: null, account: OPERATOR }))).toBe('/forbidden');
    });

    it('sends a non-operator to /forbidden for an operator page', async () => {
      expect(await go('/operator-page')).toBe('/forbidden');
    });

    it('lets an operator through an operator page', async () => {
      expect(await go('/operator-page', sessionView({ account: OPERATOR }))).toBe('/operator-page');
    });

    it('lets the path fall through to /notfound when the flag is off, absent or false', async () => {
      expect(await go('/flagged-page', sessionView({ flags: {} }))).toBe('/notfound');
    });

    it('lets the path fall through when the flag is explicitly false', async () => {
      expect(await go('/flagged-page', sessionView({ flags: { beta: false } }))).toBe('/notfound');
    });

    it('opens the path when the flag is on', async () => {
      expect(await go('/flagged-page', sessionView({ flags: { beta: true } }))).toBe('/flagged-page');
    });

    it('reports the tenant first', async () => {
      expect(await go('/everything-page', sessionView({ activeTenant: null, account: VIEWER }))).toBe('/no-tenant');
    });

    it('sends a non-operator to /forbidden when the role and the flag are unmet too', async () => {
      expect(await go('/everything-page')).toBe('/forbidden');
    });

    it('reports the role before the flag', async () => {
      expect(await go('/everything-page', sessionView({ account: { ...OPERATOR, role: 'viewer' }, flags: {} }))).toBe('/forbidden');
    });

    it('sends the person to the error page when the session cannot be loaded', async () => {
      expect(await go('/admin-page', 'server-error')).toBe('/error-page');
    });
  });

  describe('flagGuard', () => {
    // flagGuard does not load the session: it reads what the store already holds.
    async function goLoaded(url: string, view: object): Promise<string> {
      await loadSession(view);
      await router.navigateByUrl(url);

      return router.url;
    }

    it('is false for an absent key and for an empty flag set', async () => {
      expect(await goLoaded('/flag-only-page', sessionView({ flags: {} }))).toBe('/notfound');
    });

    it('is false for a flag that is off', async () => {
      expect(await goLoaded('/flag-only-page', sessionView({ flags: { beta: false } }))).toBe('/notfound');
    });

    it('is true for a flag that is on', async () => {
      expect(await goLoaded('/flag-only-page', sessionView({ flags: { beta: true } }))).toBe('/flag-only-page');
    });
  });
});
