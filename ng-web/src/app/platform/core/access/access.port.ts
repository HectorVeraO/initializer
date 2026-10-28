import { InjectionToken, Injectable, Signal, inject, signal } from '@angular/core';
import { Router, UrlTree } from '@angular/router';
import type { AccessContext, SignInOptions } from './access.types';

/** The shell's source of "who is visiting". Bound by the module that owns sessions; core ships `OPEN_ACCESS` for the others. */
export interface AccessProvider {
  /** True while the visitor may enter the shell (signed in; always true for OPEN_ACCESS). */
  readonly admitted: Signal<boolean>;
  /** The signed-in visitor; null while unknown, anonymous, or under OPEN_ACCESS. */
  readonly context: Signal<AccessContext | null>;
  /** Loads state once; resolves true, or the UrlTree for sign-in (anonymous) or /error-page (failed load). */
  admit(): Promise<true | UrlTree>;
  /** Makes `tenantId` active; rejects with the HttpErrorResponse of a refusal. */
  switchTenant(tenantId: string): Promise<void>;
  /** Re-reads the visitor after an action that changed memberships. Rejects on 0/5xx. */
  reload(): Promise<void>;
  /** Ends the session and leaves the app. Never rejects. */
  signOut(): Promise<void>;
  /** Navigates to the sign-in page; navigation state carries `email` and `notice`; `returnUrl` is a query parameter. */
  signIn(options: SignInOptions): Promise<void>;
}

/** Access without sessions: everyone is admitted, nobody has a context. There is no sign-in page, so `signIn` goes home. */
@Injectable({ providedIn: 'root' })
class OpenAccess implements AccessProvider {
  private readonly router = inject(Router);

  readonly admitted = signal(true).asReadonly();
  readonly context = signal<AccessContext | null>(null).asReadonly();

  admit(): Promise<true | UrlTree> {
    return Promise.resolve(true);
  }

  switchTenant(): Promise<void> {
    return Promise.reject(new Error('PLATFORM_NO_SESSION'));
  }

  reload(): Promise<void> {
    return Promise.resolve();
  }

  signOut(): Promise<void> {
    return Promise.resolve();
  }

  async signIn(): Promise<void> {
    await this.router.navigateByUrl('/home');
  }
}

/**
 * Bound when no module sets `ports.access`: admitted() is true, context() is null, admit() resolves true,
 * switchTenant rejects Error('PLATFORM_NO_SESSION'), reload and signOut resolve, signIn navigates to /home.
 * A token rather than a constant because signIn needs the Router.
 */
export const OPEN_ACCESS = new InjectionToken<AccessProvider>('OPEN_ACCESS', { providedIn: 'root', factory: () => inject(OpenAccess) });

export const ACCESS_PROVIDER = new InjectionToken<AccessProvider>('ACCESS_PROVIDER', { providedIn: 'root', factory: () => inject(OPEN_ACCESS) });
