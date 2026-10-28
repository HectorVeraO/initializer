import { configGuard } from '@/core/config/config.guard';
import { anonymousGuard, requireSession } from './guards/guards';
import type { WebModule } from './composition/web-module';
import type { SessionRequirement } from './guards/requirement';
import { PLATFORM_CORE_I18N } from './i18n/scope';
import { SessionAccess } from './session/session-access';
import { TenancySelection } from './tenants/tenancy-selection';
import { TenantsApi } from './tenants/tenants-api.service';

const ADMIN_OF_TENANT: SessionRequirement = { tenant: 'required', roles: ['admin'] };

const TENANT_REQUIRED: SessionRequirement = { tenant: 'required' };

const PLATFORM_OPERATOR: SessionRequirement = { platformOperator: true };

/** The core module: session, guards, composition, login, the no-tenant page and the invitation, membership and tenant screens. Other screens are added by the tracks that own them. */
export const coreModule: WebModule = {
  id: 'core',
  shellRoutes: [
    {
      path: 'no-tenant',
      canMatch: [requireSession({})],
      loadComponent: () => import('./no-tenant/no-tenant-page.component').then((m) => m.NoTenantPage)
    },
    {
      path: 'memberships',
      canMatch: [requireSession(TENANT_REQUIRED)],
      loadComponent: () => import('./memberships/memberships-page.component').then((m) => m.MembershipsPage)
    },
    {
      path: 'invitations',
      canMatch: [requireSession(ADMIN_OF_TENANT)],
      loadComponent: () => import('./invitations/invitations-page.component').then((m) => m.InvitationsPage)
    },
    {
      path: 'api-keys',
      canMatch: [requireSession(ADMIN_OF_TENANT)],
      loadComponent: () => import('./api-keys/api-keys-page.component').then((m) => m.ApiKeysPage)
    },
    {
      path: 'operator/tenants',
      canMatch: [requireSession(PLATFORM_OPERATOR)],
      loadComponent: () => import('./tenants/operator-tenants-page.component').then((m) => m.OperatorTenantsPage)
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
  nav: [
    { id: 'memberships', labelKey: 'nav.memberships', icon: 'menu-icon menu-icon-users', path: '/memberships', requires: TENANT_REQUIRED },
    { id: 'invitations', labelKey: 'nav.invitations', icon: 'menu-icon menu-icon-file-text', path: '/invitations', requires: ADMIN_OF_TENANT },
    { id: 'api-keys', labelKey: 'nav.apiKeys', icon: 'menu-icon menu-icon-layers', path: '/api-keys', requires: ADMIN_OF_TENANT },
    { id: 'operator-tenants', labelKey: 'nav.operatorTenants', icon: 'menu-icon menu-icon-building', path: '/operator/tenants', requires: PLATFORM_OPERATOR }
  ],
  i18n: PLATFORM_CORE_I18N,
  ports: { access: SessionAccess, flags: SessionAccess, tenants: TenantsApi, tenantSelection: TenancySelection }
};
