import type { Routes } from '@angular/router';

/** Routes under `/parameters`: the list is the only screen. */
export const parametersRoutes: Routes = [{ path: '', loadComponent: () => import('./parameter-list/parameter-list.component').then((m) => m.ParameterListPage) }];
