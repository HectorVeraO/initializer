import type { WebModule } from './composition/web-module';
import { PLATFORM_CORE_I18N } from './i18n/scope';

/** The core module: the shell contract (composition, access, flag and tenant ports) and its shared UI. It contributes no route, menu entry, topbar item or port. */
export const coreModule: WebModule = {
  id: 'core',
  shellRoutes: [],
  publicRoutes: [],
  nav: [],
  i18n: PLATFORM_CORE_I18N
};
