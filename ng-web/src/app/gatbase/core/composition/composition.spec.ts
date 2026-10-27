import { TestBed } from '@angular/core/testing';
import type { Route } from '@angular/router';
import { GATBASE_MODULES, GatbaseWebModule, gatbaseRoutes, provideGatbase } from './web-module';
import { coreModule } from '../module';
import { BlankComponent } from '@/core/testing/ui-testing';

function moduleWith(id: string, shell: Route[] = [], publicRoutes: Route[] = []): GatbaseWebModule {
  return { ...coreModule, id, shellRoutes: shell, publicRoutes };
}

describe('Gatbase composition', () => {
  const coreWithRoutes = moduleWith('core', [{ path: 'core-shell', component: BlankComponent }], [{ path: 'login', component: BlankComponent }]);

  const featureFlags = moduleWith('feature-flags', [{ path: 'flags', component: BlankComponent }]);

  const parameters = moduleWith('parameters', [{ path: 'parameters', component: BlankComponent }]);

  describe('gatbaseRoutes', () => {
    it('holds the core routes only for the core-only list', () => {
      const routes = gatbaseRoutes([coreModule]);

      expect(routes.shell.map((route) => route.path)).toEqual(['no-tenant', 'invitations']);
      expect(routes.public.map((route) => route.path)).toEqual(['login', 'invitations/accept']);
    });

    it('keeps the core routes and puts the others after them, in list order', () => {
      const routes = gatbaseRoutes([coreWithRoutes, featureFlags, parameters]);

      expect(routes.shell.map((route) => route.path)).toEqual(['core-shell', 'flags', 'parameters']);
      expect(routes.public.map((route) => route.path)).toEqual(['login']);
    });

    it('throws GATBASE_DUPLICATE_MODULE with the id when an id repeats', () => {
      expect(() => gatbaseRoutes([coreModule, parameters, parameters])).toThrow('GATBASE_DUPLICATE_MODULE:parameters');
    });

    it('throws GATBASE_CORE_MODULE_FIRST when the list does not start with core', () => {
      expect(() => gatbaseRoutes([parameters, coreModule])).toThrow('GATBASE_CORE_MODULE_FIRST');
    });

    it('throws GATBASE_CORE_MODULE_FIRST for an empty list', () => {
      expect(() => gatbaseRoutes([])).toThrow('GATBASE_CORE_MODULE_FIRST');
    });

    it('reports core twice as a duplicate', () => {
      expect(() => gatbaseRoutes([coreModule, coreModule])).toThrow('GATBASE_DUPLICATE_MODULE:core');
    });
  });

  describe('provideGatbase', () => {
    it('exposes the module list to the menu', () => {
      TestBed.configureTestingModule({ providers: [provideGatbase([coreModule, parameters])] });

      expect(TestBed.inject(GATBASE_MODULES).map((module) => module.id)).toEqual(['core', 'parameters']);
    });

    it('throws the same errors as gatbaseRoutes', () => {
      expect(() => provideGatbase([parameters])).toThrow('GATBASE_CORE_MODULE_FIRST');
      expect(() => provideGatbase([coreModule, parameters, parameters])).toThrow('GATBASE_DUPLICATE_MODULE:parameters');
    });
  });
});
