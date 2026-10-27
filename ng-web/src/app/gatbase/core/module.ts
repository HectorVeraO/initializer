import type { ProviderScope } from '@jsverse/transloco';
import type { GatbaseWebModule } from './composition/web-module';

/** Transloco scope of the core module; keys are read as `gatbaseCore.<key>`. */
export const GATBASE_CORE_I18N: ProviderScope = {
  scope: 'gatbase-core',
  alias: 'gatbaseCore',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

/** The core module: session, guards and composition. Screens are added by the tracks that own them. */
export const coreModule: GatbaseWebModule = {
  id: 'core',
  shellRoutes: [],
  publicRoutes: [],
  nav: [],
  i18n: GATBASE_CORE_I18N
};
