import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the core `ui/` components and the menu entries; keys are read as `core.<key>`. */
export const PLATFORM_CORE_I18N: ProviderScope = {
  scope: 'core',
  alias: 'core',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
