import { inject } from '@angular/core';
import { CanMatchFn, Router } from '@angular/router';
import { SessionStore } from '../session/session-store.service';

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
