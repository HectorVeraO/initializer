import { Injectable, Signal, computed, inject } from '@angular/core';
import { Router, UrlTree } from '@angular/router';
import type { AccessContext, AccessProvider, FlagSource, SignInOptions } from '@/platform/core';
import { SessionStore } from '../session/session-store.service';
import type { SessionView } from '../session/session.types';

const LOGIN_PATH = '/login';

const ERROR_PATH = '/error-page';

function toAccessContext(view: SessionView): AccessContext {
  const context: AccessContext = {
    account: { id: view.account.id, email: view.account.email, fullName: view.account.fullName },
    isPlatformOperator: view.account.isPlatformOperator,
    activeTenant: view.activeTenant,
    role: view.account.role,
    memberships: view.memberships
  };

  return context;
}

/** The session store seen through the shell's access and flag ports. */
@Injectable({ providedIn: 'root' })
export class SessionAccess implements AccessProvider, FlagSource {
  private readonly store = inject(SessionStore);
  private readonly router = inject(Router);

  readonly admitted: Signal<boolean> = computed(() => this.store.state() === 'authenticated');

  readonly context: Signal<AccessContext | null> = computed(() => {
    const view = this.store.session();

    const isAuthenticated = this.store.state() === 'authenticated';

    return isAuthenticated && view !== null ? toAccessContext(view) : null;
  });

  /** True once the session is loaded and authenticated; otherwise the redirect (login with the URL asked for, or the error page when the session could not be loaded or came back empty). */
  async admit(): Promise<true | UrlTree> {
    try {
      await this.store.ensureLoaded();
    } catch {
      return this.router.createUrlTree([ERROR_PATH]);
    }

    if (this.store.state() === 'anonymous') {
      const target = this.router.currentNavigation()?.extractedUrl;

      const queryParams = target === undefined ? {} : { returnUrl: this.router.serializeUrl(target) };

      return this.router.createUrlTree([LOGIN_PATH], { queryParams });
    }

    if (this.store.session() === null) {
      return this.router.createUrlTree([ERROR_PATH]);
    }

    return true;
  }

  switchTenant(tenantId: string): Promise<void> {
    return this.store.switchTenant(tenantId);
  }

  reload(): Promise<void> {
    return this.store.reload();
  }

  signOut(): Promise<void> {
    return this.store.logout();
  }

  async signIn(options: SignInOptions): Promise<void> {
    const queryParams = options.returnUrl ? { returnUrl: options.returnUrl } : {};

    const state = { ...(options.email !== undefined && { email: options.email }), ...(options.notice !== undefined && { noticeKey: options.notice }) };

    await this.router.navigate([LOGIN_PATH], { queryParams, state });
  }

  /** Default-closed: true only when the session's `flags[flagKey]` is true. */
  isEnabled(flagKey: string): boolean {
    return this.store.isEnabled(flagKey);
  }
}
