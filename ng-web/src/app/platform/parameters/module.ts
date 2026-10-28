import type { ProviderScope } from '@jsverse/transloco';
import { requireSession } from '@/platform/core';
import type { WebModule } from '@/platform/core';

/** Transloco scope of the parameters module; keys are read as `parameters.<key>`. */
export const PLATFORM_PARAMETERS_I18N: ProviderScope = {
  scope: 'parameters',
  alias: 'parameters',
  loader: { en: () => import('./i18n/en.json').then((m) => m.default), es: () => import('./i18n/es.json').then((m) => m.default) }
};

/** The parameters module: the parameter list, the tenant override dialog and the definition dialog. */
export const parametersModule: WebModule = {
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
  i18n: PLATFORM_PARAMETERS_I18N
};
