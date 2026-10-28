import { inject } from '@angular/core';
import { CanMatchFn, Router, UrlTree } from '@angular/router';
import { SessionStore } from '../session/session-store.service';
import { SessionRequirement, firstUnmetRequirement } from './requirement';

export { meetsRequirement } from './requirement';
export type { SessionRequirement } from './requirement';

/** True when the session is loaded and authenticated; otherwise the redirect (login, or the error page when the session could not be loaded). */
async function resolveAuth(store: SessionStore, router: Router): Promise<true | UrlTree> {
  try {
    await store.ensureLoaded();
  } catch {
    return router.createUrlTree(['/error-page']);
  }

  if (store.state() === 'anonymous') {
    const target = router.currentNavigation()?.extractedUrl;

    const queryParams = target === undefined ? {} : { returnUrl: router.serializeUrl(target) };

    return router.createUrlTree(['/login'], { queryParams });
  }

  return true;
}

/** Anonymous -> `/login?returnUrl=<navigation URL>`; config failure or 0/5xx -> `/error-page`. Navigation UX only: the backend enforces every rule. */
export const authGuard: CanMatchFn = () => {
  const outcome = resolveAuth(inject(SessionStore), inject(Router));

  return outcome;
};

/** For `/login`: authenticated -> `/home`; config failure or 0/5xx -> `/error-page`. */
export const anonymousGuard: CanMatchFn = async () => {
  const store = inject(SessionStore);

  const router = inject(Router);

  try {
    await store.ensureLoaded();
  } catch {
    return router.createUrlTree(['/error-page']);
  }

  return store.state() === 'authenticated' ? router.createUrlTree(['/home']) : true;
};

/**
 * `authGuard` first, then in this order: tenant unmet -> `/no-tenant`; platformOperator unmet -> `/forbidden`;
 * roles unmet -> `/forbidden`; flag unmet -> `false` (the path looks missing).
 */
export function requireSession(requirement: SessionRequirement): CanMatchFn {
  return async () => {
    const store = inject(SessionStore);

    const router = inject(Router);

    const auth = await resolveAuth(store, router);

    const session = store.session();

    if (auth !== true) {
      return auth;
    }

    if (session === null) {
      return router.createUrlTree(['/error-page']);
    }

    const unmet = firstUnmetRequirement(session, requirement);

    switch (unmet) {
      case 'tenant':
        return router.createUrlTree(['/no-tenant']);
      case 'platformOperator':
      case 'roles':
        return router.createUrlTree(['/forbidden']);
      case 'flag':
        return false;
      default:
        return true;
    }
  };
}

/** `requireSession({ flag })` without the auth part; an unknown key is false. */
export function flagGuard(flagKey: string): CanMatchFn {
  return () => inject(SessionStore).isEnabled(flagKey);
}
