import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the parameters module; keys are read as `parameters.<key>`. */
export const PLATFORM_PARAMETERS_I18N: ProviderScope = {
  scope: 'parameters',
  alias: 'parameters',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
