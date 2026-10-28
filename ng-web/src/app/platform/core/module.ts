import { configGuard } from '@/core/config/config.guard';
import { requireAccess } from './access/require-access';
import type { AccessRequirement } from './access/access.types';
import { anonymousGuard } from './guards/guards';
import type { WebModule } from './composition/web-module';
import { PLATFORM_CORE_I18N } from './i18n/scope';
import { SessionAccess } from './session/session-access';
import { platformSessionInterceptor } from './session/session.interceptor';
import { AccountMenu } from './shell/account-menu.component';
import { TenantSwitcher } from './shell/tenant-switcher.component';
import { TenancySelection } from './tenants/tenancy-selection';
import { TenantsApi } from './tenants/tenants-api.service';

const ADMIN_OF_TENANT: AccessRequirement = { tenant: 'required', roles: ['admin'] };

const TENANT_REQUIRED: AccessRequirement = { tenant: 'required' };

const PLATFORM_OPERATOR: AccessRequirement = { platformOperator: true };

/** The core module: session, guards, composition, login, the no-tenant page and the invitation, membership and tenant screens. Other screens are added by the tracks that own them. */
export const coreModule: WebModule = {
  id: 'core',
  shellRoutes: [
    {
      path: 'no-tenant',
      canMatch: [requireAccess({})],
      loadComponent: () => import('./no-tenant/no-tenant-page.component').then((m) => m.NoTenantPage)
    },
    {
      path: 'memberships',
      canMatch: [requireAccess(TENANT_REQUIRED)],
      loadComponent: () => import('./memberships/memberships-page.component').then((m) => m.MembershipsPage)
    },
    {
      path: 'invitations',
      canMatch: [requireAccess(ADMIN_OF_TENANT)],
      loadComponent: () => import('./invitations/invitations-page.component').then((m) => m.InvitationsPage)
    },
    {
      path: 'api-keys',
      canMatch: [requireAccess(ADMIN_OF_TENANT)],
      loadComponent: () => import('./api-keys/api-keys-page.component').then((m) => m.ApiKeysPage)
    },
    {
      path: 'operator/tenants',
      canMatch: [requireAccess(PLATFORM_OPERATOR)],
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
  topbar: [
    { id: 'tenant-switcher', slot: 'context', component: TenantSwitcher },
    { id: 'account-menu', slot: 'account', component: AccountMenu }
  ],
  interceptors: [platformSessionInterceptor],
  ports: { access: SessionAccess, flags: SessionAccess, tenants: TenantsApi, tenantSelection: TenancySelection }
};
