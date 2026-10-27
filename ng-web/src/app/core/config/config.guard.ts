import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { RuntimeConfigService } from './runtime-config.service';

/** Sends the person to the error page when `runtime-config.json` could not be used. */
export const configGuard: CanActivateFn = async () => {
  const runtimeConfig = inject(RuntimeConfigService);

  const router = inject(Router);

  await runtimeConfig.load();

  if (runtimeConfig.failure() === null) {
    return true;
  }

  return router.createUrlTree(['/error-page']);
};
