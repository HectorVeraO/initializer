import { Route, Routes, UrlMatchResult, UrlSegment } from '@angular/router';
import { configGuard } from '@/core/config/config.guard';
import { AppPublicLayout } from '@/layout/component/public-layout/app.public-layout';
import { AppLayout } from '@/layout/component/layout/app.layout';
import { Notfound } from '@/pages/notfound/notfound';
import { ErrorPageComponent } from '@/error-page/error-page.component';
import { DEV_ROUTES } from '@/dev/dev.routes';
import { HomeComponent } from '@/home/home.component';
import { ForbiddenComponent } from '@/forbidden/forbidden.component';
import { authGuard, gatbaseRoutes } from '@/gatbase/core';
import type { GatbaseWebModule } from '@/gatbase/core';
import { gatbaseModules } from '@/gatbase/modules';

/** Matches, without consuming anything, a URL that starts with the path of one of `publicRoutes` (a path may span several segments, such as `invitations/accept`), so the frame wraps those pages only. */
function publicPathMatcher(publicRoutes: Routes): Route['matcher'] {
  const prefixes = publicRoutes.map((route) => (route.path ?? '').split('/').filter((part) => part !== ''));

  return (segments: UrlSegment[]): UrlMatchResult | null => {
    const isPublicPath = prefixes.some((prefix) => prefix.length > 0 && prefix.length <= segments.length && prefix.every((part, index) => segments[index].path === part));

    return isPublicPath ? { consumed: [] } : null;
  };
}

/** Route table of the app, given the routes mounted under `/dev` and the installed Gatbase modules (core first). */
export function buildAppRoutes(devRoutes: Routes, modules: readonly GatbaseWebModule[]): Routes {
  const gatbase = gatbaseRoutes(modules);

  // notfound and error-page come before the layout: its guarded empty-path group would otherwise ask for the session first, and an anonymous visitor could not reach them.
  return [
    ...(gatbase.public.length > 0 ? [{ matcher: publicPathMatcher(gatbase.public), component: AppPublicLayout, children: gatbase.public }] : []),
    { path: 'notfound', component: Notfound },
    { path: 'error-page', component: ErrorPageComponent },
    {
      path: '',
      component: AppLayout,
      canActivate: [configGuard],
      children: [
        { path: '', pathMatch: 'full', redirectTo: 'home' },
        { path: 'forbidden', component: ForbiddenComponent },
        { path: 'dev', children: devRoutes },
        { path: '', canMatch: [authGuard], children: [{ path: 'home', component: HomeComponent }, ...gatbase.shell] }
      ]
    },
    { path: '**', redirectTo: '/notfound' }
  ];
}

export const appRoutes: Routes = buildAppRoutes(DEV_ROUTES, gatbaseModules);
