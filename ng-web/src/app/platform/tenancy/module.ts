import { configGuard } from '@/core/config/config.guard';
import { requireAccess } from '@/platform/core';
import type { AccessRequirement, WebModule } from '@/platform/core';
import { TENANCY_I18N } from './i18n/scope';
import { TenancySelection } from './selection/tenancy-selection';
import { TenantSwitcher } from './shell/tenant-switcher.component';
import { TenantsApi } from './tenants/tenants-api.service';

const TENANT_REQUIRED: AccessRequirement = { tenant: 'required' };

const ADMIN_OF_TENANT: AccessRequirement = { tenant: 'required', roles: ['admin'] };

const PLATFORM_OPERATOR: AccessRequirement = { platformOperator: true };

/** The tenancy module: tenants, memberships and invitations, the tenant switcher and the no-tenant page. It binds the shell's tenant directory and tenant selection ports. */
export const tenancyModule: WebModule = {
  id: 'tenancy',
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
      path: 'operator/tenants',
      canMatch: [requireAccess(PLATFORM_OPERATOR)],
      loadComponent: () => import('./tenants/operator-tenants-page.component').then((m) => m.OperatorTenantsPage)
    }
  ],
  publicRoutes: [
    {
      path: 'invitations/accept',
      canActivate: [configGuard],
      loadComponent: () => import('./invitations/accept-invitation-page.component').then((m) => m.AcceptInvitationPage)
    }
  ],
  nav: [
    { id: 'memberships', labelKey: 'nav.memberships', icon: 'menu-icon menu-icon-users', path: '/memberships', requires: TENANT_REQUIRED },
    { id: 'invitations', labelKey: 'nav.invitations', icon: 'menu-icon menu-icon-file-text', path: '/invitations', requires: ADMIN_OF_TENANT },
    { id: 'operator-tenants', labelKey: 'nav.operatorTenants', icon: 'menu-icon menu-icon-building', path: '/operator/tenants', requires: PLATFORM_OPERATOR }
  ],
  i18n: TENANCY_I18N,
  topbar: [{ id: 'tenant-switcher', slot: 'context', component: TenantSwitcher }],
  ports: { tenants: TenantsApi, tenantSelection: TenancySelection }
};
