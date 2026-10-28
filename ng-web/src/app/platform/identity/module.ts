import { configGuard } from '@/core/config/config.guard';
import type { WebModule } from '@/platform/core';
import { SessionAccess } from './access/session-access';
import { anonymousGuard } from './guards/anonymous.guard';
import { IDENTITY_I18N } from './i18n/scope';
import { sessionInterceptor } from './session/session.interceptor';
import { AccountMenu } from './shell/account-menu.component';

/** The identity module: sign-in with Kratos, the stateless session, and the account menu. It binds the shell's access and flag ports. */
export const identityModule: WebModule = {
  id: 'identity',
  shellRoutes: [],
  publicRoutes: [
    {
      path: 'login',
      canActivate: [configGuard],
      canMatch: [anonymousGuard],
      loadComponent: () => import('./login/login-page.component').then((m) => m.LoginPage)
    }
  ],
  nav: [],
  i18n: IDENTITY_I18N,
  topbar: [{ id: 'account-menu', slot: 'account', component: AccountMenu }],
  interceptors: [sessionInterceptor],
  ports: { access: SessionAccess, flags: SessionAccess }
};
