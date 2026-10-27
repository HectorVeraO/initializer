import type { ProviderScope } from '@jsverse/transloco';
import { requireSession } from '@/gatbase/core';
import type { GatbaseWebModule } from '@/gatbase/core';

/** Transloco scope of the feature-flags module; keys are read as `gatbaseFeatureFlags.<key>`. */
export const GATBASE_FEATURE_FLAGS_I18N: ProviderScope = {
  scope: 'gatbase-feature-flags',
  alias: 'gatbaseFeatureFlags',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

/** The feature-flags module: the operator flag list with the global override, and the flag detail with tenant overrides. */
export const featureFlagsModule: GatbaseWebModule = {
  id: 'feature-flags',
  shellRoutes: [
    {
      path: 'operator/feature-flags',
      canMatch: [requireSession({ platformOperator: true })],
      loadChildren: () => import('./feature-flags.routes').then((m) => m.featureFlagsRoutes)
    }
  ],
  publicRoutes: [],
  nav: [{ id: 'feature-flags', labelKey: 'nav.featureFlags', icon: 'menu-icon menu-icon-tag', path: '/operator/feature-flags', requires: { platformOperator: true } }],
  i18n: GATBASE_FEATURE_FLAGS_I18N
};
