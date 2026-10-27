import type { ProviderScope } from '@jsverse/transloco';
import { configGuard } from '@/core/config/config.guard';
import { anonymousGuard, requireSession } from './guards/guards';
import type { GatbaseWebModule } from './composition/web-module';
import type { SessionRequirement } from './guards/requirement';

/** Transloco scope of the core module; keys are read as `gatbaseCore.<key>`. */
export const GATBASE_CORE_I18N: ProviderScope = {
  scope: 'gatbase-core',
  alias: 'gatbaseCore',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

const ADMIN_OF_TENANT: SessionRequirement = { tenant: 'required', roles: ['admin'] };

/** The core module: session, guards, composition, login, the no-tenant page and the invitation screens. Other screens are added by the tracks that own them. */
export const coreModule: GatbaseWebModule = {
  id: 'core',
  shellRoutes: [
    {
      path: 'no-tenant',
      canMatch: [requireSession({})],
      loadComponent: () => import('./no-tenant/no-tenant-page.component').then((m) => m.NoTenantPage)
    },
    {
      path: 'invitations',
      canMatch: [requireSession(ADMIN_OF_TENANT)],
      loadComponent: () => import('./invitations/invitations-page.component').then((m) => m.InvitationsPage)
    }
  ],
  publicRoutes: [
    {
      path: 'login',
      canActivate: [configGuard],
      canMatch: [anonymousGuard],
      loadComponent: () => import('./login/login-page.component').then((m) => m.LoginPage)
    },
    {
      path: 'invitations/accept',
      canActivate: [configGuard],
      loadComponent: () => import('./invitations/accept-invitation-page.component').then((m) => m.AcceptInvitationPage)
    }
  ],
  nav: [{ id: 'invitations', labelKey: 'nav.invitations', icon: 'menu-icon menu-icon-file-text', path: '/invitations', requires: ADMIN_OF_TENANT }],
  i18n: GATBASE_CORE_I18N
};
