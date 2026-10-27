import type { ProviderScope } from '@jsverse/transloco';
import { configGuard } from '@/core/config/config.guard';
import { anonymousGuard, requireSession } from './guards/guards';
import type { GatbaseWebModule } from './composition/web-module';

/** Transloco scope of the core module; keys are read as `gatbaseCore.<key>`. */
export const GATBASE_CORE_I18N: ProviderScope = {
  scope: 'gatbase-core',
  alias: 'gatbaseCore',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

/** The core module: session, guards, composition, login and the no-tenant page. Other screens are added by the tracks that own them. */
export const coreModule: GatbaseWebModule = {
  id: 'core',
  shellRoutes: [
    {
      path: 'no-tenant',
      canMatch: [requireSession({})],
      loadComponent: () => import('./no-tenant/no-tenant-page.component').then((m) => m.NoTenantPage)
    }
  ],
  publicRoutes: [
    {
      path: 'login',
      canActivate: [configGuard],
      canMatch: [anonymousGuard],
      loadComponent: () => import('./login/login-page.component').then((m) => m.LoginPage)
    }
  ],
  nav: [],
  i18n: GATBASE_CORE_I18N
};
