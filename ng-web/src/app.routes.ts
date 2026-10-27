import { Routes } from '@angular/router';
import { configGuard } from '@/core/config/config.guard';
import { AppLayout } from '@/layout/component/layout/app.layout';
import { Notfound } from '@/pages/notfound/notfound';
import { ErrorPageComponent } from '@/error-page/error-page.component';
import { DEV_ROUTES } from '@/dev/dev.routes';
import { HomeComponent } from '@/home/home.component';
import { ForbiddenComponent } from '@/forbidden/forbidden.component';

/** Route table of the app, given the routes mounted under `/dev`. */
export function buildAppRoutes(devRoutes: Routes): Routes {
  return [
    {
      path: '',
      component: AppLayout,
      canActivate: [configGuard],
      children: [
        { path: '', pathMatch: 'full', redirectTo: 'home' },
        { path: 'home', component: HomeComponent },
        { path: 'forbidden', component: ForbiddenComponent },
        { path: 'dev', children: devRoutes }
      ]
    },
    { path: 'notfound', component: Notfound },
    { path: 'error-page', component: ErrorPageComponent },
    { path: '**', redirectTo: '/notfound' }
  ];
}

export const appRoutes: Routes = buildAppRoutes(DEV_ROUTES);
