import { HttpErrorResponse } from '@angular/common/http';
import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { CONFIG_URL, FakeBroadcastChannel, SESSION_URL, fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from './browser-location';
import { SessionStore } from './session-store.service';

const SECOND_TENANT = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

const FIRST_TENANT_ID = '11111111-1111-4111-8111-111111111111';

describe('SessionStore', () => {
  let messages: { add: ReturnType<typeof vi.fn> };

  let location: ReturnType<typeof fakeLocation>;

  let http: HttpTestingController;

  beforeEach(() => {
    stubBroadcastChannel();
    messages = { add: vi.fn() };
    location = fakeLocation();
    configureFeatureTest(
      [{ path: '**', component: BlankComponent }],
      [
        { provide: MessageService, useValue: messages },
        { provide: BROWSER_LOCATION, useValue: location }
      ]
    );
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  function store(): SessionStore {
    return TestBed.inject(SessionStore);
  }

  describe('loading', () => {
    it('is anonymous after a 401', async () => {
      await loadSession(null);

      expect(store().state()).toBe('anonymous');
      expect(store().session()).toBeNull();
      expect(store().account()).toBeNull();
      expect(store().role()).toBeNull();
      expect(store().isPlatformOperator()).toBe(false);
    });

    it('fills account, role, tenant and flags after a 200', async () => {
      await loadSession(sessionView({ flags: { beta: true, hidden: false } }));

      expect(store().state()).toBe('authenticated');
      expect(store().account()?.email).toBe('admin@example.com');
      expect(store().role()).toBe('admin');
      expect(store().activeTenant()?.slug).toBe('tenant-a');
      expect(store().activeTenantId()).toBe('11111111-1111-4111-8111-111111111111');
      expect(store().isEnabled('beta')).toBe(true);
    });

    it('treats an unknown, false or inherited flag key as off', async () => {
      await loadSession(sessionView({ flags: { hidden: false } }));

      expect(store().isEnabled('missing')).toBe(false);
      expect(store().isEnabled('hidden')).toBe(false);
      expect(store().isEnabled('constructor')).toBe(false);
      expect(store().isEnabled('__proto__')).toBe(false);
    });

    it('has no flag on before the session loaded', () => {
      expect(store().state()).toBe('unknown');
      expect(store().isEnabled('beta')).toBe(false);
    });

    it('copes with no active tenant and no memberships', async () => {
      const account = { id: 'a', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true };

      await loadSession(sessionView({ activeTenant: null, memberships: [], account }));

      expect(store().activeTenant()).toBeNull();
      expect(store().activeTenantId()).toBeNull();
      expect(store().role()).toBeNull();
      expect(store().isPlatformOperator()).toBe(true);
      expect(store().switchableMemberships()).toEqual([]);
    });

    it('offers only active memberships of active tenants to the switcher', async () => {
      const active = { id: 'm1', tenant: SECOND_TENANT, role: 'member', status: 'active' };

      const suspendedTenant = { id: 'm2', tenant: { ...SECOND_TENANT, id: 'x', slug: 'tenant-c', status: 'suspended' }, role: 'member', status: 'active' };

      const revoked = { id: 'm3', tenant: { ...SECOND_TENANT, id: 'y', slug: 'tenant-d' }, role: 'member', status: 'revoked' };

      await loadSession(sessionView({ memberships: [active, suspendedTenant, revoked] }));

      expect(
        store()
          .switchableMemberships()
          .map((membership) => membership.id)
      ).toEqual(['m1']);
    });

    it('loads once for concurrent callers and not again afterwards', async () => {
      const first = store().ensureLoaded();

      const second = store().ensureLoaded();

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await Promise.all([first, second]);
      await store().ensureLoaded();

      http.expectNone(SESSION_URL);

      expect(store().state()).toBe('authenticated');
    });

    it('rejects without calling the API when the runtime config failed', async () => {
      const loaded = store().ensureLoaded();

      const outcome = expect(loaded).rejects.toThrow('PLATFORM_CONFIG_UNAVAILABLE');

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' }));
      await outcome;

      http.expectNone(SESSION_URL);

      expect(store().state()).toBe('unknown');
    });

    it('rejects on a server failure, keeps the state and can load again', async () => {
      const loaded = store().ensureLoaded();

      const outcome = expect(loaded).rejects.toBeInstanceOf(HttpErrorResponse);

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
      await outcome;

      expect(store().state()).toBe('unknown');

      const retried = store().ensureLoaded();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await retried;

      expect(store().state()).toBe('authenticated');
    });

    it('turns a locked account into anonymous and hands the denial over once', async () => {
      const loaded = store().ensureLoaded();

      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('ACCOUNT_LOCKED_OR_DISABLED'), { status: 403, statusText: 'Forbidden' });
      await loaded;

      expect(store().state()).toBe('anonymous');
      expect(store().takeDenial()).toBe('ACCOUNT_LOCKED_OR_DISABLED');
      expect(store().takeDenial()).toBeNull();
    });
  });

  describe('switchTenant', () => {
    it('posts the tenant id and adopts the answer', async () => {
      await loadSession();

      const done = store().switchTenant(SECOND_TENANT.id);

      const request = await nextCall('POST', '/api/v1/auth/switch-tenant');

      expect(request.request.body).toEqual({ tenantId: SECOND_TENANT.id });
      request.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await done;

      expect(store().activeTenant()?.slug).toBe('tenant-b');
    });

    it('tells the other tabs', async () => {
      await loadSession();

      const done = store().switchTenant(SECOND_TENANT.id);

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await done;

      expect(FakeBroadcastChannel.instances[0].posted).toEqual([{ type: 'session-changed', tenantId: SECOND_TENANT.id, state: 'authenticated' }]);
    });

    it('reloads the session and rejects with the error on a 403', async () => {
      await loadSession();

      const done = store().switchTenant(SECOND_TENANT.id);

      const outcome = expect(done).rejects.toMatchObject({ status: 403 });

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('TENANT_SUSPENDED'), { status: 403, statusText: 'Forbidden' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await outcome;

      expect(store().activeTenant()?.slug).toBe('tenant-a');
    });

    it('rejects on a 422 without reloading', async () => {
      await loadSession();

      const done = store().switchTenant('not-a-uuid');

      const outcome = expect(done).rejects.toMatchObject({ status: 422 });

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('VALIDATION_FAILED'), { status: 422, statusText: 'Unprocessable' });
      await outcome;

      http.expectNone(SESSION_URL);
    });
  });

  describe('ordering', () => {
    it('keeps the tenant a switch confirmed when an older load answers after it', async () => {
      await loadSession();

      const reload = store().reload();

      const staleLoad = await nextCall('GET', SESSION_URL);

      const switched = store().switchTenant(SECOND_TENANT.id);

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;

      // The server answered the GET before the switch, so it still names tenant A; it arrives last.
      staleLoad.flush(okEnvelope(sessionView()));
      await reload;

      expect(store().activeTenantId()).toBe(SECOND_TENANT.id);
    });

    it('does not announce the stale tenant to the other tabs', async () => {
      await loadSession();

      const reload = store().reload();

      const staleLoad = await nextCall('GET', SESSION_URL);

      const switched = store().switchTenant(SECOND_TENANT.id);

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;
      staleLoad.flush(okEnvelope(sessionView()));
      await reload;

      expect(FakeBroadcastChannel.instances[0].posted).toEqual([{ type: 'session-changed', tenantId: SECOND_TENANT.id, state: 'authenticated' }]);
    });

    it('keeps the confirmed tenant when a load started after the switch answers first with the old one', async () => {
      await loadSession();

      const switched = store().switchTenant(SECOND_TENANT.id);

      const switchCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

      const reload = store().reload();

      const laterLoad = await nextCall('GET', SESSION_URL);

      laterLoad.flush(okEnvelope(sessionView()));
      await reload;
      switchCall.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;

      expect(store().activeTenantId()).toBe(SECOND_TENANT.id);
    });

    it('does not re-authenticate when a pending load answers after logout', async () => {
      await loadSession();

      const reload = store().reload();

      const pendingLoad = await nextCall('GET', SESSION_URL);

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;
      pendingLoad.flush(okEnvelope(sessionView()));
      await reload;

      expect(store().state()).toBe('anonymous');
    });

    it('does not re-authenticate when a pending load answers after a refresh lost the session', async () => {
      await loadSession();

      const reload = store().reload();

      const pendingLoad = await nextCall('GET', SESSION_URL);

      const refreshed = store().refresh();

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(errorEnvelope('UNAUTHENTICATED'), { status: 401, statusText: 'Unauthorized' });
      await refreshed;
      pendingLoad.flush(okEnvelope(sessionView()));
      await reload;

      expect(store().state()).toBe('anonymous');
    });

    it('drops an older 401 that answers after a newer load', async () => {
      await loadSession();

      const first = store().reload();

      const olderLoad = await nextCall('GET', SESSION_URL);

      const second = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await second;
      olderLoad.flush(errorEnvelope('UNAUTHENTICATED'), { status: 401, statusText: 'Unauthorized' });
      await first;

      expect(store().state()).toBe('authenticated');
      expect(store().activeTenantId()).toBe(SECOND_TENANT.id);
    });

    it('does not re-authenticate when a pending switch answers after logout', async () => {
      await loadSession();

      const switched = store().switchTenant(SECOND_TENANT.id);

      const switchCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;
      switchCall.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;

      expect(store().state()).toBe('anonymous');
    });

    it('keeps the newer switch when an older switch answers after it', async () => {
      await loadSession();

      const first = store().switchTenant(SECOND_TENANT.id);

      const olderCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

      const second = store().switchTenant(FIRST_TENANT_ID);

      (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView()));
      await second;
      olderCall.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await first;

      expect(store().activeTenantId()).toBe(FIRST_TENANT_ID);
    });

    it('does not let a switch started before a logout overwrite a session signed in again', async () => {
      await loadSession();

      const switched = store().switchTenant(SECOND_TENANT.id);

      const switchCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;

      const reload = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await reload;
      switchCall.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;

      expect(store().activeTenantId()).toBe(FIRST_TENANT_ID);
    });

    it('still adopts a load that started after the switch', async () => {
      await loadSession();

      const switched = store().switchTenant(SECOND_TENANT.id);

      const switchCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

      const reload = store().reload();

      const laterLoad = await nextCall('GET', SESSION_URL);

      switchCall.flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await switched;
      laterLoad.flush(okEnvelope(sessionView()));
      await reload;

      expect(store().activeTenantId()).toBe(FIRST_TENANT_ID);
    });

    it('ends anonymous when a load started after the logout answers before the logout', async () => {
      await loadSession();

      const done = store().logout();

      const logoutCall = await nextCall('POST', '/api/v1/auth/logout');

      const reload = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await reload;
      logoutCall.flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;

      expect(store().state()).toBe('anonymous');
    });
  });

  describe('refresh', () => {
    it('adopts the refreshed session', async () => {
      await loadSession();

      const outcome = store().refresh();

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(okEnvelope(sessionView({ flags: { beta: true } })));

      expect(await outcome).toBe('refreshed');
      expect(store().isEnabled('beta')).toBe(true);
    });

    it('goes anonymous on a 401', async () => {
      await loadSession();

      const outcome = store().refresh();

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });

      expect(await outcome).toBe('anonymous');
      expect(store().state()).toBe('anonymous');
    });

    it('drops a refresh answer that arrives after a newer load', async () => {
      await loadSession();

      const refreshed = store().refresh();

      const refreshCall = await nextCall('POST', '/api/v1/auth/refresh');

      const reload = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await reload;
      refreshCall.flush(okEnvelope(sessionView()));
      await refreshed;

      expect(store().activeTenantId()).toBe(SECOND_TENANT.id);
    });

    it('goes anonymous on a 401 even when a newer load answered first', async () => {
      await loadSession();

      const outcome = store().refresh();

      const refreshCall = await nextCall('POST', '/api/v1/auth/refresh');

      const reload = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await reload;
      refreshCall.flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });

      expect(await outcome).toBe('anonymous');
      expect(store().state()).toBe('anonymous');
    });

    it('reloads on a 403 and reports denied', async () => {
      await loadSession();

      const outcome = store().refresh();

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(errorEnvelope('TENANT_SUSPENDED'), { status: 403, statusText: 'Forbidden' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: null, account: { id: 'a', email: 'admin@example.com', fullName: null, role: null, isPlatformOperator: false } })));

      expect(await outcome).toBe('denied');
      expect(store().activeTenant()).toBeNull();
    });

    it('rejects on a server failure', async () => {
      await loadSession();

      const outcome = store().refresh();

      const rejected = expect(outcome).rejects.toBeInstanceOf(HttpErrorResponse);

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
      await rejected;
    });

    it('shares one request between concurrent callers', async () => {
      await loadSession();

      const first = store().refresh();

      const second = store().refresh();

      (await nextCall('POST', '/api/v1/auth/refresh')).flush(okEnvelope(sessionView()));

      expect(await Promise.all([first, second])).toEqual(['refreshed', 'refreshed']);
    });
  });

  describe('logout', () => {
    it('posts, clears the session and goes to the logout URL', async () => {
      await loadSession();

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: '/kratos/self-service/logout?token=synthetic-logout' }));
      await done;

      expect(store().state()).toBe('anonymous');
      expect(store().session()).toBeNull();
      expect(location.assign).toHaveBeenCalledWith('/kratos/self-service/logout?token=synthetic-logout');
      expect(FakeBroadcastChannel.instances[0].posted).toEqual([{ type: 'session-changed', tenantId: null, state: 'anonymous' }]);
    });

    it('counts a 401 as already logged out', async () => {
      await loadSession();

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });
      await done;

      expect(store().state()).toBe('anonymous');
      expect(location.assign).toHaveBeenCalledWith('/login');
    });

    it('still ends on /login when the call fails', async () => {
      await loadSession();

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Server Error' });
      await done;

      expect(store().state()).toBe('anonymous');
      expect(location.assign).toHaveBeenCalledWith('/login');
    });

    it.each(['https://evil.example.com/steal', '//evil.example.com/steal', 'javascript:alert(1)', ''])('does not follow the off-origin or unusable logout URL %j', async (logoutUrl) => {
      await loadSession();

      const done = store().logout();

      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl }));
      await done;

      expect(location.assign).toHaveBeenCalledWith('/login');
    });
  });

  describe('other tabs', () => {
    it('reloads on a session-changed message and moves to /home with a notice when the tenant changed', async () => {
      await loadSession();

      FakeBroadcastChannel.instances[0].deliver({ type: 'session-changed', tenantId: SECOND_TENANT.id, state: 'authenticated' });

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));

      await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'El tenant activo cambió en otra pestaña.' })));

      expect(TestBed.inject(Router).url).toBe('/home');
      expect(store().activeTenant()?.slug).toBe('tenant-b');
      // The reload caused by a message is not announced again.
      expect(FakeBroadcastChannel.instances[0].posted).toEqual([]);
    });

    it('moves to /login when the other tab logged out', async () => {
      await loadSession();

      FakeBroadcastChannel.instances[0].deliver({ type: 'session-changed', tenantId: null, state: 'anonymous' });

      (await nextCall('GET', SESSION_URL)).flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });

      await vi.waitFor(() => expect(store().state()).toBe('anonymous'));
      await vi.waitFor(() => expect(TestBed.inject(Router).url).toBe('/login'));

      expect(messages.add).not.toHaveBeenCalled();
    });

    it('does not navigate or notify when the reload changed nothing', async () => {
      await loadSession();

      FakeBroadcastChannel.instances[0].deliver({ type: 'session-changed', tenantId: null, state: 'authenticated' });

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
      await vi.waitFor(() => expect(store().state()).toBe('authenticated'));

      expect(messages.add).not.toHaveBeenCalled();
    });

    it.each([{ type: 'other', tenantId: null, state: 'anonymous' }, { type: 'session-changed', tenantId: 5, state: 'anonymous' }, { type: 'session-changed', tenantId: null, state: 'bogus' }, 'session-changed', null])(
      'ignores the malformed message %j',
      async (message) => {
        await loadSession();

        FakeBroadcastChannel.instances[0].deliver(message);

        http.expectNone(SESSION_URL);
      }
    );

    it('ignores a message in a tab that never loaded the session', () => {
      store();

      FakeBroadcastChannel.instances[0].deliver({ type: 'session-changed', tenantId: null, state: 'anonymous' });

      http.expectNone(SESSION_URL);
    });

    it('announces a change found by a later reload but not the first load', async () => {
      await loadSession();

      expect(FakeBroadcastChannel.instances[0].posted).toEqual([]);

      const reloaded = store().reload();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ activeTenant: SECOND_TENANT })));
      await reloaded;

      expect(FakeBroadcastChannel.instances[0].posted).toEqual([{ type: 'session-changed', tenantId: SECOND_TENANT.id, state: 'authenticated' }]);
    });

    it('closes its channel when the app is destroyed', async () => {
      await loadSession();

      TestBed.resetTestingModule();

      expect(FakeBroadcastChannel.instances[0].closed).toBe(true);
    });

    it('works without BroadcastChannel', async () => {
      vi.stubGlobal('BroadcastChannel', undefined);

      await loadSession();

      expect(store().state()).toBe('authenticated');
    });
  });

  describe('when the tab becomes visible', () => {
    let now: number;

    beforeEach(() => {
      now = 1_000_000;
      vi.spyOn(Date, 'now').mockImplementation(() => now);
    });

    function becomeVisible(): void {
      document.dispatchEvent(new Event('visibilitychange'));
    }

    it('reloads a session older than 30 seconds', async () => {
      await loadSession();
      now += 31_000;
      becomeVisible();

      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView()));
    });

    it('leaves a session exactly 30 seconds old alone', async () => {
      await loadSession();
      now += 30_000;
      becomeVisible();

      http.expectNone(SESSION_URL);
    });

    it('leaves a fresher session alone', async () => {
      await loadSession();
      now += 5_000;
      becomeVisible();

      http.expectNone(SESSION_URL);
    });

    it('does nothing before the first load', () => {
      store();
      now += 60_000;
      becomeVisible();

      http.expectNone(SESSION_URL);
    });
  });
});
