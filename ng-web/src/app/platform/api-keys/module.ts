import { requireAccess } from '@/platform/core';
import type { AccessRequirement, WebModule } from '@/platform/core';
import { API_KEYS_I18N } from './i18n/scope';

/** Web mirror of the Go api-keys policy (admin role, or a platform operator with no active tenant). */
const KEY_ADMIN: AccessRequirement = { tenant: 'required', roles: ['admin'], orTenantlessOperator: true };

/** The api-keys module: the keys screen. Keys belong to the active tenant, or are global for a tenantless platform operator. */
export const apiKeysModule: WebModule = {
  id: 'api-keys',
  shellRoutes: [
    {
      path: 'api-keys',
      canMatch: [requireAccess(KEY_ADMIN)],
      loadComponent: () => import('./api-keys-page.component').then((m) => m.ApiKeysPage)
    }
  ],
  publicRoutes: [],
  nav: [{ id: 'api-keys', labelKey: 'nav.apiKeys', icon: 'menu-icon menu-icon-layers', path: '/api-keys', requires: KEY_ADMIN }],
  i18n: API_KEYS_I18N
};
