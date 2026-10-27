import { Routes } from '@angular/router';

/**
 * Developer guide routes (Sakai dashboard and UI kit). The production build
 * replaces this file with `dev.routes.prod.ts`, so the code is not bundled.
 */
export const DEV_ROUTES: Routes = [
  { path: 'dashboard', loadChildren: () => import('@/pages/dashboard/dashboard.routes').then((m) => m.dashboardRoutes) },
  { path: 'uikit', loadChildren: () => import('@/pages/uikit/uikit.routes') }
];
