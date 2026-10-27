import { HttpClient, HttpContext, HttpContextToken, HttpErrorResponse } from '@angular/common/http';
import { DestroyRef, Injectable, Signal, computed, inject, signal } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { Router } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { Observable, firstValueFrom } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorCode } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope } from '@/core/envelope';
import { BROWSER_LOCATION } from './browser-location';
import { notifyTenantChanged } from './session-notice';
import type { SessionState } from './session-state';
import { SessionChangedMessage, SessionSync } from './session-sync';
import type { LogoutResult, MembershipRole, SessionAccount, SessionMembership, SessionView, TenantSummary } from './session.types';

export type { SessionState } from './session-state';

/** Marks GET /auth/session, POST /auth/refresh and POST /auth/logout: never refreshed, no stale-tab header. */
export const SESSION_LIFECYCLE = new HttpContextToken<boolean>(() => false);

/** Marks POST /auth/switch-tenant: no stale-tab header; refresh-and-retry still applies. */
export const NO_TENANT_HEADER = new HttpContextToken<boolean>(() => false);

/** A session older than this is reloaded when its tab becomes visible. */
const STALE_AFTER_MS = 30_000;

const LOGIN_PATH = '/login';

/** True when `target` stays on the origin of this page. */
function isSameOriginTarget(target: unknown, origin: string): target is string {
  if (typeof target !== 'string' || target === '') {
    return false;
  }

  try {
    return new URL(target, origin).origin === origin;
  } catch {
    return false;
  }
}

/** Single owner of the browser's view of the BFF session. Holds no token: the session cookie is HttpOnly. */
@Injectable({ providedIn: 'root' })
export class SessionStore {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);
  private readonly router = inject(Router);
  private readonly messages = inject(MessageService);
  private readonly transloco = inject(TranslocoService);
  private readonly browserLocation = inject(BROWSER_LOCATION);
  private readonly document = inject(DOCUMENT);

  private readonly _state = signal<SessionState>('unknown');
  private readonly _session = signal<SessionView | null>(null);
  private readonly sync: SessionSync;

  private loading: Promise<void> | null = null;
  private refreshing: Promise<'refreshed' | 'anonymous' | 'denied'> | null = null;
  private lastDenial: string | null = null;
  private lastLoadAt = 0;
  // Each request that can replace the session takes a number when it starts; an answer older than the last committed one is dropped.
  private lastStarted = 0;
  private lastCommitted = 0;
  private lastSwitchCommitted = 0;
  private lastAnonymousCommitted = 0;

  readonly state: Signal<SessionState> = this._state.asReadonly();
  readonly session: Signal<SessionView | null> = this._session.asReadonly();
  readonly account: Signal<SessionAccount | null> = computed(() => this._session()?.account ?? null);
  readonly activeTenant: Signal<TenantSummary | null> = computed(() => this._session()?.activeTenant ?? null);
  readonly activeTenantId: Signal<string | null> = computed(() => this.activeTenant()?.id ?? null);
  /** `account.role`. */
  readonly role: Signal<MembershipRole | null> = computed(() => this.account()?.role ?? null);
  readonly isPlatformOperator: Signal<boolean> = computed(() => this.account()?.isPlatformOperator === true);
  /** Memberships the switcher offers: membership `active` and tenant `active`. */
  readonly switchableMemberships: Signal<readonly SessionMembership[]> = computed(() => (this._session()?.memberships ?? []).filter((membership) => membership.status === 'active' && membership.tenant.status === 'active'));

  constructor() {
    this.sync = new SessionSync(() => {
      void this.onSessionChanged();
    });

    const onVisibility = (): void => {
      void this.reloadWhenStale();
    };

    this.document.addEventListener('visibilitychange', onVisibility);
    inject(DestroyRef).onDestroy(() => {
      this.document.removeEventListener('visibilitychange', onVisibility);
      this.sync.close();
    });
  }

  /** Default-closed: true only when `flags[flagKey] === true`. */
  isEnabled(flagKey: string): boolean {
    return this._session()?.flags[flagKey] === true;
  }

  /** First load, single-flight. Rejects with `GATBASE_CONFIG_UNAVAILABLE` when the runtime config failed, and with the HttpErrorResponse on 0/5xx. */
  ensureLoaded(): Promise<void> {
    if (this._state() !== 'unknown') {
      return Promise.resolve();
    }

    if (this.loading === null) {
      this.loading = this.firstLoad().finally(() => {
        this.loading = null;
      });
    }

    return this.loading;
  }

  /** GET /auth/session. Rejects on 0/5xx and leaves the state unchanged. */
  async reload(): Promise<void> {
    await this.load(true);
  }

  /** Adopts the `200 SessionView` answer as the new session. */
  async switchTenant(tenantId: string): Promise<void> {
    const context = new HttpContext().set(NO_TENANT_HEADER, true);

    const ticket = ++this.lastStarted;

    try {
      const view = await this.call<SessionView>(this.http.post<ApiEnvelope<SessionView>>(this.runtimeConfig.apiPath('/api/v1/auth/switch-tenant'), { tenantId }, { context }));

      // The server confirmed this tenant, so it wins over a load that started later but read the old one; a logout or a lost session still wins over it, and an older switch never overwrites a newer one.
      // A session lost after this switch started (even if a later sign-in restored one) is not the session this answer belongs to.
      const isStillSignedIn = this._state() === 'authenticated' && this.lastAnonymousCommitted < ticket;

      const isLatestSwitch = ticket >= this.lastSwitchCommitted;

      if (isLatestSwitch) {
        this.lastSwitchCommitted = ticket;
      }

      this.commit('authenticated', view, false, ticket, isStillSignedIn && isLatestSwitch);
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 403) {
        await this.load(true).catch(() => undefined);
      }

      throw error;
    }

    this.announce();
  }

  /** POST /auth/refresh, single-flight. */
  refresh(): Promise<'refreshed' | 'anonymous' | 'denied'> {
    if (this.refreshing === null) {
      this.refreshing = this.doRefresh().finally(() => {
        this.refreshing = null;
      });
    }

    return this.refreshing;
  }

  /** POST /auth/logout, then leaves the app. Never rejects: a failed call still ends on /login. */
  async logout(): Promise<void> {
    const context = new HttpContext().set(SESSION_LIFECYCLE, true);

    let target = LOGIN_PATH;

    const ticket = ++this.lastStarted;

    try {
      const result = await this.call<LogoutResult>(this.http.post<ApiEnvelope<LogoutResult>>(this.runtimeConfig.apiPath('/api/v1/auth/logout'), {}, { context }));

      // The URL comes from the backend; one that leaves this origin is not followed.
      if (isSameOriginTarget(result?.logoutUrl, this.browserLocation.origin)) {
        target = result.logoutUrl;
      }
    } catch {
      // 401 means already logged out; any other failure also ends on /login.
    }

    // Logout is final: the tab leaves the app, so the store must agree even when a load that started later answered first.
    this.commit('anonymous', null, false, ticket, true);
    this.announce();
    this.browserLocation.assign(target);
  }

  /** Code of the last 403 that ended a session (shown once on /login), then cleared. */
  takeDenial(): string | null {
    const denial = this.lastDenial;

    this.lastDenial = null;

    return denial;
  }

  private async firstLoad(): Promise<void> {
    await this.runtimeConfig.load();

    if (this.runtimeConfig.failure() !== null) {
      throw new Error('GATBASE_CONFIG_UNAVAILABLE');
    }

    await this.load(true);
  }

  /** Loads the session; resolves whether the state or the active tenant changed. */
  private async load(broadcast: boolean): Promise<boolean> {
    const context = new HttpContext().set(SESSION_LIFECYCLE, true).set(SKIP_ERROR_TOAST, true);

    const ticket = ++this.lastStarted;

    try {
      const view = await this.call<SessionView>(this.http.get<ApiEnvelope<SessionView>>(this.runtimeConfig.apiPath('/api/v1/auth/session'), { context }));

      return this.commit('authenticated', view, broadcast, ticket);
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 401) {
        return this.commit('anonymous', null, broadcast, ticket);
      }

      if (error instanceof HttpErrorResponse && error.status === 403 && errorCode(error) === ApiErrorCode.AccountLockedOrDisabled) {
        this.lastDenial = ApiErrorCode.AccountLockedOrDisabled;

        return this.commit('anonymous', null, broadcast, ticket);
      }

      throw error;
    }
  }

  private async doRefresh(): Promise<'refreshed' | 'anonymous' | 'denied'> {
    const context = new HttpContext().set(SESSION_LIFECYCLE, true);

    const ticket = ++this.lastStarted;

    try {
      const view = await this.call<SessionView>(this.http.post<ApiEnvelope<SessionView>>(this.runtimeConfig.apiPath('/api/v1/auth/refresh'), {}, { context }));

      this.commit('authenticated', view, true, ticket);

      return 'refreshed';
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 401) {
        // A refresh 401 is final: the caller leaves for /login, so the store must agree even when a newer load answered first.
        this.commit('anonymous', null, true, ticket, true);

        return 'anonymous';
      }

      if (error instanceof HttpErrorResponse && error.status === 403) {
        await this.load(true).catch(() => undefined);

        return 'denied';
      }

      throw error;
    }
  }

  private call<T>(request: Observable<ApiEnvelope<T>>): Promise<T> {
    const data = firstValueFrom(request.pipe(unwrapData<T>()));

    return data;
  }

  /** True when no request that started later has committed yet. */
  private isCurrent(ticket: number): boolean {
    const isCurrent = ticket >= this.lastCommitted;

    return isCurrent;
  }

  /** Stores the new state and returns whether the state or the active tenant changed; an answer older than the last committed one is dropped and changes nothing. */
  private commit(state: SessionState, view: SessionView | null, broadcast: boolean, ticket: number, force = false): boolean {
    if (!force && !this.isCurrent(ticket)) {
      return false;
    }

    this.lastCommitted = Math.max(this.lastCommitted, ticket);

    if (state === 'anonymous') {
      this.lastAnonymousCommitted = Math.max(this.lastAnonymousCommitted, ticket);
    }

    const before = { state: this._state(), tenantId: this.activeTenantId() };

    this._session.set(view);
    this._state.set(state);
    this.lastLoadAt = Date.now();

    const changed = before.state !== state || before.tenantId !== this.activeTenantId();

    // A first load is not a change other tabs need to hear about.
    if (broadcast && changed && before.state !== 'unknown') {
      this.announce();
    }

    return changed;
  }

  private announce(): void {
    const message: SessionChangedMessage = { type: 'session-changed', tenantId: this.activeTenantId(), state: this._state() };

    this.sync.post(message);
  }

  private async onSessionChanged(): Promise<void> {
    // A tab that never loaded the session has nothing to reconcile.
    if (this._state() === 'unknown') {
      return;
    }

    const wasAuthenticated = this._state() === 'authenticated';

    let changed: boolean;

    try {
      changed = await this.load(false);
    } catch {
      return;
    }

    if (!changed) {
      return;
    }

    const target = this._state() === 'anonymous' ? LOGIN_PATH : '/home';

    await this.router.navigateByUrl(target).catch(() => false);

    // Only a move between tenants gets the notice; a logout or a login elsewhere just navigates.
    const isTenantChange = wasAuthenticated && this._state() === 'authenticated';

    if (isTenantChange) {
      await notifyTenantChanged(this.messages, this.transloco);
    }
  }

  private async reloadWhenStale(): Promise<void> {
    const isVisible = this.document.visibilityState === 'visible';

    const isStale = Date.now() - this.lastLoadAt > STALE_AFTER_MS;

    if (!isVisible || !isStale || this._state() === 'unknown') {
      return;
    }

    await this.reload().catch(() => undefined);
  }
}
