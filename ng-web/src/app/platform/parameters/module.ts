import { requireAccess } from '@/platform/core';
import type { WebModule } from '@/platform/core';
import { PLATFORM_PARAMETERS_I18N } from './i18n/scope';

/** The parameters module: the parameter list, the tenant override dialog and the definition dialog. */
export const parametersModule: WebModule = {
  id: 'parameters',
  shellRoutes: [
    {
      path: 'parameters',
      canMatch: [requireAccess({ tenant: 'required-unless-operator' })],
      loadChildren: () => import('./parameters.routes').then((m) => m.parametersRoutes)
    }
  ],
  publicRoutes: [],
  nav: [{ id: 'parameters', labelKey: 'nav.parameters', icon: 'menu-icon menu-icon-sliders', path: '/parameters', requires: { tenant: 'required-unless-operator' } }],
  i18n: PLATFORM_PARAMETERS_I18N
};
