import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the API keys screen; keys are read as `apiKeys.<key>`. */
export const API_KEYS_I18N: ProviderScope = {
  scope: 'api-keys',
  alias: 'apiKeys',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
