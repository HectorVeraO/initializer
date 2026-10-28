import { HttpErrorResponse } from '@angular/common/http';
import { HttpTestingController } from '@angular/common/http/testing';
import { inject } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope, okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { SESSION_URL, fakeLocation, loadSession } from '../../../../testing/fixtures/session-testing';
import { CONFIG_URL, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { BROWSER_LOCATION } from './browser-location';
import { SessionAccess } from './session-access';

const OPERATOR = { id: '22222222-2222-4222-8222-222222222222', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true };

describe('SessionAccess', () => {
  let http: HttpTestingController;

  let router: Router;

  let location: ReturnType<typeof fakeLocation>;

  beforeEach(() => {
    stubBroadcastChannel();
    location = fakeLocation();
    configureFeatureTest(
      [
        { path: 'private/:id', canMatch: [() => inject(SessionAccess).admit()], component: BlankComponent },
        { path: 'login', component: BlankComponent },
        { path: 'error-page', component: BlankComponent },
        { path: '**', component: BlankComponent }
      ],
      [
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: location }
      ]
    );
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  function access(): SessionAccess {
    return TestBed.inject(SessionAccess);
  }

  describe('admit', () => {
    async function visit(url: string, answer: object | 'anonymous' | 'server-error' | 'config-error' | 'empty-session'): Promise<string> {
      const navigation = router.navigateByUrl(url);

      if (answer === 'config-error') {
        await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' }));
      } else {
        await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));

        const request = await nextCall('GET', SESSION_URL);

        if (answer === 'anonymous') {
          request.flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });
        } else if (answer === 'server-error') {
          request.flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
        } else if (answer === 'empty-session') {
          request.flush(okEnvelope(null));
        } else {
          request.flush(okEnvelope(answer));
        }
      }

      await navigation;

      return router.url;
    }

    it('lets a signed-in visitor through', async () => {
      expect(await visit('/private/42', sessionView())).toBe('/private/42');
    });

    it('sends an anonymous visitor to /login and keeps the URL asked for', async () => {
      expect(await visit('/private/42?x=1', 'anonymous')).toBe('/login?returnUrl=%2Fprivate%2F42%3Fx%3D1');
    });

    it('sends the visitor to the error page when the runtime config failed', async () => {
      expect(await visit('/private/42', 'config-error')).toBe('/error-page');
      http.expectNone(SESSION_URL);
    });

    it('sends the visitor to the error page when the session cannot be loaded', async () => {
      expect(await visit('/private/42', 'server-error')).toBe('/error-page');
    });

    it('sends the visitor to the error page when the session answers empty', async () => {
      expect(await visit('/private/42', 'empty-session')).toBe('/error-page');
    });
  });

  describe('admitted and context', () => {
    it('are closed before the session is loaded', () => {
      expect(access().admitted()).toBe(false);
      expect(access().context()).toBeNull();
    });

    it('are closed for an anonymous visitor', async () => {
      await loadSession(null);

      expect(access().admitted()).toBe(false);
      expect(access().context()).toBeNull();
    });

    it('map the session view', async () => {
      await loadSession();

      expect(access().admitted()).toBe(true);
      expect(access().context()).toEqual({
        account: { id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', fullName: 'Jane Doe' },
        isPlatformOperator: false,
        activeTenant: { id: '11111111-1111-4111-8111-111111111111', slug: 'tenant-a', name: 'Tenant A', status: 'active' },
        role: 'admin',
        memberships: [
          {
            id: '33333333-3333-4333-8333-333333333333',
            tenant: { id: '11111111-1111-4111-8111-111111111111', slug: 'tenant-a', name: 'Tenant A', status: 'active' },
            role: 'admin',
            status: 'active'
          }
        ]
      });
    });

    it('carry null tenant fields and no memberships when the view has none', async () => {
      await loadSession(sessionView({ activeTenant: null, memberships: [], account: OPERATOR }));

      expect(access().context()).toEqual({
        account: { id: OPERATOR.id, email: 'op@example.com', fullName: null },
        isPlatformOperator: true,
        activeTenant: null,
        role: null,
        memberships: []
      });
    });
  });

  describe('switchTenant', () => {
    it('posts the tenant id and adopts the answer', async () => {
      await loadSession();

      const switched = access().switchTenant('44444444-4444-4444-8444-444444444444');

      const request = await nextCall('POST', '/api/v1/auth/switch-tenant');

      expect(request.request.body).toEqual({ tenantId: '44444444-4444-4444-8444-444444444444' });
      request.flush(okEnvelope(sessionView({ activeTenant: { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' } })));
      await switched;

      expect(access().context()?.activeTenant?.slug).toBe('tenant-b');
    });

    it('rejects with the HttpErrorResponse of a 403 refusal', async () => {
      await loadSession();

      const switched = access().switchTenant('44444444-4444-4444-8444-444444444444');

      const outcome = switched.then(
        () => null,
        (error: unknown) => error
      );

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('TENANT_SUSPENDED'), { status: 403, statusText: 'Forbidden' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));

      const error = await outcome;

      expect(error).toBeInstanceOf(HttpErrorResponse);
      expect((error as HttpErrorResponse).status).toBe(403);
    });
  });

  describe('reload', () => {
    it('re-reads the session', async () => {
      await loadSession();

      const reloaded = access().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ flags: { beta: true } })));
      await reloaded;

      expect(access().isEnabled('beta')).toBe(true);
    });
  });

  describe('signIn', () => {
    it('opens the login page with the email and the notice in the navigation state', async () => {
      await access().signIn({ email: 'jane@example.com', notice: 'accountCreated' });

      expect(router.url).toBe('/login');
      expect(router.lastSuccessfulNavigation()?.extras.state).toEqual({ email: 'jane@example.com', noticeKey: 'accountCreated' });
    });

    it('carries the returnUrl as a query parameter', async () => {
      await access().signIn({ returnUrl: '/invitations/accept?token=synthetic' });

      expect(router.url).toBe('/login?returnUrl=%2Finvitations%2Faccept%3Ftoken%3Dsynthetic');
    });

    it('opens a plain login page without options', async () => {
      await access().signIn({});

      expect(router.url).toBe('/login');
    });
  });

  describe('signOut', () => {
    it('posts the logout and follows the logout URL of the same origin', async () => {
      await loadSession();

      const done = access().signOut();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;

      expect(location.assign).toHaveBeenCalledWith('/kratos/self-service/logout?token=synthetic-logout');
      expect(access().admitted()).toBe(false);
    });
  });

  describe('signOut with a foreign logout URL', () => {
    it('does not follow a logout URL of another origin', async () => {
      await loadSession();

      const done = access().signOut();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: 'https://evil.example.com/logout' }));
      await done;

      expect(location.assign).not.toHaveBeenCalledWith('https://evil.example.com/logout');
      expect(access().admitted()).toBe(false);
    });
  });

  describe('isEnabled', () => {
    it('reads the flags of the session and is closed for an unknown key', async () => {
      await loadSession(sessionView({ flags: { beta: true, gamma: false } }));

      expect(access().isEnabled('beta')).toBe(true);
      expect(access().isEnabled('gamma')).toBe(false);
      expect(access().isEnabled('unknown')).toBe(false);
    });

    it('is closed while the session is not loaded', () => {
      expect(access().isEnabled('beta')).toBe(false);
    });
  });
});
