import type { ProviderScope } from '@jsverse/transloco';

/** Transloco scope of the tenant switcher and the no-tenant, invitation, membership and tenant screens; keys are read as `tenancy.<key>`. */
export const TENANCY_I18N: ProviderScope = {
  scope: 'tenancy',
  alias: 'tenancy',
  loader: { en: () => import('./en.json').then((m) => m.default), es: () => import('./es.json').then((m) => m.default) }
};
