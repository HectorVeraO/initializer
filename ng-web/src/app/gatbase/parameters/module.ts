import type { ProviderScope } from '@jsverse/transloco';
import { requireSession } from '@/gatbase/core';
import type { GatbaseWebModule } from '@/gatbase/core';

/** Transloco scope of the parameters module; keys are read as `gatbaseParameters.<key>`. */
export const GATBASE_PARAMETERS_I18N: ProviderScope = {
  scope: 'gatbase-parameters',
  alias: 'gatbaseParameters',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

/** The parameters module: the parameter list, the tenant override dialog and the definition dialog. */
export const parametersModule: GatbaseWebModule = {
  id: 'parameters',
  shellRoutes: [
    {
      path: 'parameters',
      canMatch: [requireSession({ tenant: 'required-unless-operator' })],
      loadChildren: () => import('./parameters.routes').then((m) => m.parametersRoutes)
    }
  ],
  publicRoutes: [],
  nav: [{ id: 'parameters', labelKey: 'nav.parameters', icon: 'menu-icon menu-icon-sliders', path: '/parameters', requires: { tenant: 'required-unless-operator' } }],
  i18n: GATBASE_PARAMETERS_I18N
};
