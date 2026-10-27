import { Routes } from '@angular/router';
import { configGuard } from '@/core/config/config.guard';
import { AppLayout } from '@/layout/component/layout/app.layout';
import { Notfound } from '@/pages/notfound/notfound';
import { ErrorPageComponent } from '@/error-page/error-page.component';
import { DEV_ROUTES } from '@/dev/dev.routes';
import { HomeComponent } from '@/home/home.component';
import { ForbiddenComponent } from '@/forbidden/forbidden.component';
import { authGuard, gatbaseRoutes } from '@/gatbase/core';
import type { GatbaseWebModule } from '@/gatbase/core';
import { gatbaseModules } from '@/gatbase/modules';

/** Route table of the app, given the routes mounted under `/dev` and the installed Gatbase modules (core first). */
export function buildAppRoutes(devRoutes: Routes, modules: readonly GatbaseWebModule[]): Routes {
  const gatbase = gatbaseRoutes(modules);

  // notfound and error-page come before the layout: its guarded empty-path group would otherwise ask for the session first, and an anonymous visitor could not reach them.
  return [
    ...gatbase.public,
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
