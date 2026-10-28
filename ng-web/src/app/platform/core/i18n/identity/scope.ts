import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the login page, the account menu and the session notice; keys are read as `identity.<key>`. */
export const IDENTITY_I18N: ProviderScope = {
  scope: 'identity',
  alias: 'identity',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
