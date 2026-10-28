import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { NavigationStart, Router, provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { firstValueFrom } from 'rxjs';
import es from '../../../../assets/i18n/es.json';
import { provideApiHttpClient } from '@/core/interceptors/api-http.providers';
import { errorEnvelope, okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { SESSION_URL, fakeLocation, loadSession } from '../../../../testing/fixtures/session-testing';
import { nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { TENANT_SELECTION } from '../tenants/tenant-selection.port';
import type { TenantSelection } from '../tenants/tenant-selection.port';
import { BROWSER_LOCATION } from './browser-location';
import { platformSessionInterceptor } from './session.interceptor';
import { SessionStore } from './session-store.service';
import { BlankComponent } from '@/core/testing/ui-testing';

const REFRESH_URL = '/api/v1/auth/refresh';

const TENANT_ID = '11111111-1111-4111-8111-111111111111';

const UNAUTHORIZED = { status: 401, statusText: 'Unauthorized' };

describe('platformSessionInterceptor', () => {
  let http: HttpTestingController;

  let client: HttpClient;

  let router: Router;

  let messages: { add: ReturnType<typeof vi.fn> };

  // Read when the interceptor first runs, so a spec can choose the binding before its first request.
  let selection: TenantSelection | null;

  beforeEach(async () => {
    stubBroadcastChannel();
    messages = { add: vi.fn() };
    selection = { noTenantPath: '/no-tenant' };
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [
        provideApiHttpClient([platformSessionInterceptor]),
        provideHttpClientTesting(),
        provideRouter([{ path: '**', component: BlankComponent }]),
        { provide: MessageService, useValue: messages },
        { provide: TENANT_SELECTION, useFactory: () => selection },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    });
    http = TestBed.inject(HttpTestingController);
    client = TestBed.inject(HttpClient);
    router = TestBed.inject(Router);
    await router.navigateByUrl('/reports/1');
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  /** Starts a request and keeps its outcome, so a spec can answer the call before awaiting it. */
  function call(method: 'GET' | 'POST', url: string): Promise<{ ok: true; body: unknown } | { ok: false; error: HttpErrorResponse }> {
    const request = method === 'GET' ? client.get(url) : client.post(url, {});

    const outcome = firstValueFrom(request).then(
      (body) => ({ ok: true as const, body }),
      (error: HttpErrorResponse) => ({ ok: false as const, error })
    );

    return outcome;
  }

  describe('stale-tab header', () => {
    it('adds X-Active-Tenant-Id to an API call once a tenant is active', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      const request = await nextCall('GET', '/api/v1/parameters');

      expect(request.request.headers.get('X-Active-Tenant-Id')).toBe(TENANT_ID);
      request.flush(okEnvelope([]));
      await outcome;
    });

    it('sends no header without an active tenant', async () => {
      await loadSession(sessionView({ activeTenant: null }));

      const outcome = call('GET', '/api/v1/tenants');

      const request = await nextCall('GET', '/api/v1/tenants');

      expect(request.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      request.flush(okEnvelope([]));
      await outcome;
    });

    it('sends no header on the session load, refresh and logout calls', async () => {
      await loadSession();

      const reload = TestBed.inject(SessionStore).reload();

      const load = await nextCall('GET', SESSION_URL);

      expect(load.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      load.flush(okEnvelope(sessionView()));
      await reload;

      const refresh = TestBed.inject(SessionStore).refresh();

      const refreshCall = await nextCall('POST', REFRESH_URL);

      expect(refreshCall.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      refreshCall.flush(okEnvelope(sessionView()));
      await refresh;

      const logout = TestBed.inject(SessionStore).logout();

      const logoutCall = await nextCall('POST', '/api/v1/auth/logout');

      expect(logoutCall.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      logoutCall.flush(okEnvelope({ logoutUrl: '/login' }));
      await logout;
    });

    it('sends no header on switch-tenant', async () => {
      await loadSession();

      const done = TestBed.inject(SessionStore).switchTenant(TENANT_ID);

      const request = await nextCall('POST', '/api/v1/auth/switch-tenant');

      expect(request.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      request.flush(okEnvelope(sessionView()));
      await done;
    });

    it('leaves a call to another host untouched, even on a 401', async () => {
      await loadSession();

      const outcome = call('GET', 'https://other.example.com/api/v1/x');

      const request = await nextCall('GET', 'https://other.example.com/api/v1/x');

      expect(request.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      request.flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);

      expect((await outcome).ok).toBe(false);
      http.expectNone(REFRESH_URL);
    });
  });

  describe('401 SESSION_INVALID', () => {
    it('refreshes once and retries the call once', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));

      const retry = await nextCall('GET', '/api/v1/parameters');

      retry.flush(okEnvelope(['row']));

      expect(await outcome).toEqual({ ok: true, body: okEnvelope(['row']) });
    });

    it('retries with the tenant the call started with and refuses it with 409 when the tenant changed during the refresh', async () => {
      await loadSession();

      const tenantB = { id: 'b', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

      const outcome = call('GET', '/api/v1/parameters');

      const first = await nextCall('GET', '/api/v1/parameters');

      expect(first.request.headers.get('X-Active-Tenant-Id')).toBe(TENANT_ID);
      first.flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView({ activeTenant: tenantB })));

      const retry = await nextCall('GET', '/api/v1/parameters');

      expect(retry.request.headers.get('X-Active-Tenant-Id')).toBe(TENANT_ID);
      retry.flush(errorEnvelope('ACTIVE_TENANT_CHANGED'), { status: 409, statusText: 'Conflict' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: tenantB })));

      const result = await outcome;

      expect(result.ok === false && result.error.status).toBe(409);
      http.expectNone(REFRESH_URL);
    });

    it('sends the retry through the trace interceptor again, with a fresh traceparent', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      const first = await nextCall('GET', '/api/v1/parameters');

      const firstTrace = first.request.headers.get('traceparent');

      first.flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));

      const retry = await nextCall('GET', '/api/v1/parameters');

      expect(firstTrace).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$/);
      expect(retry.request.headers.get('traceparent')).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$/);
      expect(retry.request.headers.get('traceparent')).not.toBe(firstTrace);
      retry.flush(okEnvelope([]));
      await outcome;
    });

    it('shares one refresh between concurrent 401s and retries both calls', async () => {
      await loadSession();

      const a = call('GET', '/api/v1/a');

      const b = call('GET', '/api/v1/b');

      (await nextCall('GET', '/api/v1/a')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('GET', '/api/v1/b')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));
      (await nextCall('GET', '/api/v1/a')).flush(okEnvelope('a'));
      (await nextCall('GET', '/api/v1/b')).flush(okEnvelope('b'));

      expect((await a).ok).toBe(true);
      expect((await b).ok).toBe(true);
    });

    it('surfaces a second 401 instead of looping', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));
      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);

      const result = await outcome;

      expect(result.ok === false && result.error.status).toBe(401);
      http.expectNone(REFRESH_URL);
    });

    it('refreshes again for a later 401 once an earlier retry finished', async () => {
      await loadSession();

      const first = call('GET', '/api/v1/a');

      (await nextCall('GET', '/api/v1/a')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));
      (await nextCall('GET', '/api/v1/a')).flush(okEnvelope(1));
      await first;

      // A later, separate 401 refreshes again: the retry flag did not stick.
      const second = call('GET', '/api/v1/b');

      (await nextCall('GET', '/api/v1/b')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));
      (await nextCall('GET', '/api/v1/b')).flush(okEnvelope(2));
      await second;
    });

    it('sends the user to /login with the current URL as returnUrl when the refresh is refused', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);

      const result = await outcome;

      await vi.waitFor(() => expect(router.url).toBe('/login?returnUrl=%2Freports%2F1'));

      expect(result.ok === false && result.error.status).toBe(401);
    });

    it('drops the URL fragment from returnUrl', async () => {
      await router.navigateByUrl('/reports/1?tab=a#frag=synthetic-secret');
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      await outcome;

      await vi.waitFor(() => expect(router.url).toContain('/login'));

      expect(router.url).toBe('/login?returnUrl=%2Freports%2F1%3Ftab%3Da');
      expect(router.url).not.toContain('synthetic-secret');
    });

    it('sends the user to /home when the refresh is denied', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(errorEnvelope('MEMBERSHIP_REVOKED'), { status: 403, statusText: 'Forbidden' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));

      const result = await outcome;

      await vi.waitFor(() => expect(router.url).toBe('/home'));

      expect(result.ok === false && result.error.status).toBe(401);
    });

    it('surfaces the original 401 when the refresh itself fails with a server error', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });

      const result = await outcome;

      expect(result.ok === false && result.error.status).toBe(401);
      expect(router.url).toBe('/reports/1');
    });

    it('never refreshes after a 401 that is not SESSION_INVALID', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('API_KEY_INVALID_OR_REVOKED'), UNAUTHORIZED);

      expect((await outcome).ok).toBe(false);
      http.expectNone(REFRESH_URL);
    });

    it('never refreshes after a 401 of the session load itself', async () => {
      await loadSession();

      const reload = TestBed.inject(SessionStore).reload();

      (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      await reload;

      http.expectNone(REFRESH_URL);

      expect(TestBed.inject(SessionStore).state()).toBe('anonymous');
    });

    it('refreshes and retries switch-tenant once', async () => {
      await loadSession();

      const done = TestBed.inject(SessionStore).switchTenant(TENANT_ID);

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('SESSION_INVALID'), UNAUTHORIZED);
      (await nextCall('POST', REFRESH_URL)).flush(okEnvelope(sessionView()));

      const retry = await nextCall('POST', '/api/v1/auth/switch-tenant');

      expect(retry.request.headers.has('X-Active-Tenant-Id')).toBe(false);
      retry.flush(okEnvelope(sessionView()));
      await done;
    });
  });

  describe('409 ACTIVE_TENANT_CHANGED', () => {
    it('shows the notice, reloads, goes home and does not retry', async () => {
      await loadSession();

      const outcome = call('POST', '/api/v1/parameters');

      (await nextCall('POST', '/api/v1/parameters')).flush(errorEnvelope('ACTIVE_TENANT_CHANGED'), { status: 409, statusText: 'Conflict' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: { id: 'b', slug: 'tenant-b', name: 'Tenant B', status: 'active' } })));

      const result = await outcome;

      await vi.waitFor(() => expect(router.url).toBe('/home'));

      expect(result.ok === false && result.error.status).toBe(409);
      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'El tenant activo cambió en otra pestaña.' }));
      expect(TestBed.inject(SessionStore).activeTenant()?.slug).toBe('tenant-b');
      http.expectNone('/api/v1/parameters');
    });
  });

  describe('403', () => {
    it('sends a GET answering ACTIVE_TENANT_REQUIRED to /no-tenant', async () => {
      await loadSession(sessionView({ activeTenant: null }));

      const outcome = call('GET', '/api/v1/members');

      (await nextCall('GET', '/api/v1/members')).flush(errorEnvelope('ACTIVE_TENANT_REQUIRED'), { status: 403, statusText: 'Forbidden' });

      const result = await outcome;

      await vi.waitFor(() => expect(router.url).toBe('/no-tenant'));

      expect(result.ok === false && result.error.status).toBe(403);
    });

    it('sends a GET answering ACTIVE_TENANT_REQUIRED to the bound noTenantPath', async () => {
      selection = { noTenantPath: '/choose-tenant' };
      await loadSession(sessionView({ activeTenant: null }));

      const outcome = call('GET', '/api/v1/members');

      (await nextCall('GET', '/api/v1/members')).flush(errorEnvelope('ACTIVE_TENANT_REQUIRED'), { status: 403, statusText: 'Forbidden' });
      await outcome;

      await vi.waitFor(() => expect(router.url).toBe('/choose-tenant'));
    });

    it('does not navigate when no tenant selection is bound, and the error surfaces', async () => {
      selection = null;
      await loadSession(sessionView({ activeTenant: null }));

      const starts: string[] = [];

      const subscription = router.events.subscribe((event) => {
        if (event instanceof NavigationStart) {
          starts.push(event.url);
        }
      });

      const outcome = call('GET', '/api/v1/members');

      (await nextCall('GET', '/api/v1/members')).flush(errorEnvelope('ACTIVE_TENANT_REQUIRED'), { status: 403, statusText: 'Forbidden' });

      const result = await outcome;

      // A navigation started by the interceptor would begin within one macrotask.
      await new Promise((resolve) => setTimeout(resolve, 0));
      subscription.unsubscribe();

      expect(result.ok === false && result.error.status).toBe(403);
      expect(starts).toEqual([]);
      expect(router.url).toBe('/reports/1');
    });

    it('leaves a POST answering ACTIVE_TENANT_REQUIRED where it is', async () => {
      await loadSession(sessionView({ activeTenant: null }));

      const outcome = call('POST', '/api/v1/members');

      (await nextCall('POST', '/api/v1/members')).flush(errorEnvelope('ACTIVE_TENANT_REQUIRED'), { status: 403, statusText: 'Forbidden' });
      await outcome;

      expect(router.url).toBe('/reports/1');
    });

    it('rethrows CSRF_TOKEN_INVALID without a refresh, a retry or a navigation', async () => {
      await loadSession();

      const outcome = call('POST', '/api/v1/parameters');

      (await nextCall('POST', '/api/v1/parameters')).flush(errorEnvelope('CSRF_TOKEN_INVALID'), { status: 403, statusText: 'Forbidden' });

      const result = await outcome;

      expect(result.ok === false && result.error.status).toBe(403);
      expect(router.url).toBe('/reports/1');
      http.expectNone(REFRESH_URL);
      http.expectNone('/api/v1/parameters');
    });

    it('rethrows an unrelated error unchanged', async () => {
      await loadSession();

      const outcome = call('GET', '/api/v1/parameters');

      (await nextCall('GET', '/api/v1/parameters')).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Not Found' });

      const result = await outcome;

      expect(result.ok === false && result.error.status).toBe(404);
      expect(router.url).toBe('/reports/1');
    });
  });
});
