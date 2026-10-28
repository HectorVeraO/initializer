import { TestBed } from '@angular/core/testing';
import type { Route } from '@angular/router';
import { PLATFORM_MODULES, WebModule, platformRoutes, providePlatform } from './web-module';
import { coreModule } from '../module';
import { BlankComponent } from '@/core/testing/ui-testing';

function moduleWith(id: string, shell: Route[] = [], publicRoutes: Route[] = []): WebModule {
  return { ...coreModule, id, shellRoutes: shell, publicRoutes };
}

describe('Module composition', () => {
  const coreWithRoutes = moduleWith('core', [{ path: 'core-shell', component: BlankComponent }], [{ path: 'login', component: BlankComponent }]);

  const flagsModule = moduleWith('feature-flags', [{ path: 'flags', component: BlankComponent }]);

  const parameters = moduleWith('parameters', [{ path: 'parameters', component: BlankComponent }]);

  describe('platformRoutes', () => {
    it('holds the core routes only for the core-only list', () => {
      const routes = platformRoutes([coreModule]);

      expect(routes.shell.map((route) => route.path)).toEqual(['no-tenant', 'memberships', 'invitations', 'api-keys', 'operator/tenants']);
      expect(routes.public.map((route) => route.path)).toEqual(['login', 'invitations/accept']);
    });

    it('keeps the core routes and puts the others after them, in list order', () => {
      const routes = platformRoutes([coreWithRoutes, flagsModule, parameters]);

      expect(routes.shell.map((route) => route.path)).toEqual(['core-shell', 'flags', 'parameters']);
      expect(routes.public.map((route) => route.path)).toEqual(['login']);
    });

    it('throws PLATFORM_DUPLICATE_MODULE with the id when an id repeats', () => {
      expect(() => platformRoutes([coreModule, parameters, parameters])).toThrow('PLATFORM_DUPLICATE_MODULE:parameters');
    });

    it('throws PLATFORM_CORE_MODULE_FIRST when the list does not start with core', () => {
      expect(() => platformRoutes([parameters, coreModule])).toThrow('PLATFORM_CORE_MODULE_FIRST');
    });

    it('throws PLATFORM_CORE_MODULE_FIRST for an empty list', () => {
      expect(() => platformRoutes([])).toThrow('PLATFORM_CORE_MODULE_FIRST');
    });

    it('reports core twice as a duplicate', () => {
      expect(() => platformRoutes([coreModule, coreModule])).toThrow('PLATFORM_DUPLICATE_MODULE:core');
    });
  });

  describe('providePlatform', () => {
    it('exposes the module list to the menu', () => {
      TestBed.configureTestingModule({ providers: [providePlatform([coreModule, parameters])] });

      expect(TestBed.inject(PLATFORM_MODULES).map((module) => module.id)).toEqual(['core', 'parameters']);
    });

    it('throws the same errors as platformRoutes', () => {
      expect(() => providePlatform([parameters])).toThrow('PLATFORM_CORE_MODULE_FIRST');
      expect(() => providePlatform([coreModule, parameters, parameters])).toThrow('PLATFORM_DUPLICATE_MODULE:parameters');
    });
  });
});
