import type { Routes } from '@angular/router';

/** Routes under `/operator/feature-flags`: the list and the detail of one flag. */
export const featureFlagsRoutes: Routes = [
  { path: '', loadComponent: () => import('./flag-list/flag-list.component').then((m) => m.FlagListPage) },
  { path: ':key', loadComponent: () => import('./flag-detail/flag-detail.component').then((m) => m.FlagDetailPage) }
];
