import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the feature-flags module; keys are read as `featureFlags.<key>`. */
export const PLATFORM_FEATURE_FLAGS_I18N: ProviderScope = {
  scope: 'feature-flags',
  alias: 'featureFlags',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
