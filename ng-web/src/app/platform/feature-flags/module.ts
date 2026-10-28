import { requireAccess } from '@/platform/core';
import type { WebModule } from '@/platform/core';
import { PLATFORM_FEATURE_FLAGS_I18N } from './i18n/scope';

/** The feature-flags module: the operator flag list with the global override, and the flag detail with tenant overrides. */
export const featureFlagsModule: WebModule = {
  id: 'feature-flags',
  shellRoutes: [
    {
      path: 'operator/feature-flags',
      canMatch: [requireAccess({ platformOperator: true })],
      loadChildren: () => import('./feature-flags.routes').then((m) => m.featureFlagsRoutes)
    }
  ],
  publicRoutes: [],
  nav: [{ id: 'feature-flags', labelKey: 'nav.featureFlags', icon: 'menu-icon menu-icon-tag', path: '/operator/feature-flags', requires: { platformOperator: true } }],
  i18n: PLATFORM_FEATURE_FLAGS_I18N
};
