import type { HttpInterceptorFn } from '@angular/common/http';
import { Injectable, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import type { Route } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { firstValueFrom, of } from 'rxjs';
import { ACCESS_PROVIDER, OPEN_ACCESS } from '../access/access.port';
import type { AccessProvider } from '../access/access.port';
import { FLAG_SOURCE, NO_FLAGS } from '../flags/flag-source.port';
import type { FlagSource } from '../flags/flag-source.port';
import { SessionAccess } from '../session/session-access';
import { TenancySelection } from '../tenants/tenancy-selection';
import { TENANT_DIRECTORY } from '../tenants/tenant-directory.port';
import type { TenantDirectory } from '../tenants/tenant-directory.port';
import { TENANT_SELECTION } from '../tenants/tenant-selection.port';
import type { TenantSelection } from '../tenants/tenant-selection.port';
import { TenantsApi } from '../tenants/tenants-api.service';
import { PLATFORM_MODULES, PortBindings, TopbarItem, WebModule, navLabel, platformInterceptors, platformRoutes, platformTopbar, providePlatform } from './web-module';
import { coreModule } from '../module';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';

/** One class bound to both `access` and `flags`, as identity binds `SessionAccess`. */
@Injectable({ providedIn: 'root' })
class AccessAndFlags implements AccessProvider, FlagSource {
  readonly admitted = signal(true).asReadonly();
  readonly context = signal(null).asReadonly();
  admit = (): Promise<true> => Promise.resolve(true);
  switchTenant = (): Promise<void> => Promise.resolve();
  reload = (): Promise<void> => Promise.resolve();
  signOut = (): Promise<void> => Promise.resolve();
  signIn = (): Promise<void> => Promise.resolve();
  isEnabled = (flagKey: string): boolean => flagKey === 'beta';
}

@Injectable({ providedIn: 'root' })
class OtherAccess extends AccessAndFlags {}

@Injectable({ providedIn: 'root' })
class Directory implements TenantDirectory {
  list = (): ReturnType<TenantDirectory['list']> => of({ items: [], page: 1, size: 100, totalItems: 0 });
}

@Injectable({ providedIn: 'root' })
class Selection implements TenantSelection {
  readonly noTenantPath = '/pick-tenant';
}

const traceA: HttpInterceptorFn = (request, next) => next(request);

const traceB: HttpInterceptorFn = (request, next) => next(request);

const traceC: HttpInterceptorFn = (request, next) => next(request);

function topbarItem(id: string, slot: TopbarItem['slot']): TopbarItem {
  return { id, slot, component: BlankComponent };
}

function moduleWith(id: string, shell: Route[] = [], publicRoutes: Route[] = [], extra: Partial<WebModule> = {}): WebModule {
  return { ...coreModule, id, shellRoutes: shell, publicRoutes, ports: undefined, ...extra };
}

function withPorts(id: string, ports: PortBindings): WebModule {
  return moduleWith(id, [], [], { ports });
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

  describe('port bindings', () => {
    it('throws PLATFORM_DUPLICATE_PORT for each of the four ports, naming both modules', () => {
      expect(() => providePlatform([coreModule, withPorts('other', { access: OtherAccess })])).toThrow('PLATFORM_DUPLICATE_PORT:access:core,other');
      expect(() => providePlatform([coreModule, withPorts('other', { flags: OtherAccess })])).toThrow('PLATFORM_DUPLICATE_PORT:flags:core,other');
      expect(() => providePlatform([coreModule, withPorts('other', { tenants: Directory })])).toThrow('PLATFORM_DUPLICATE_PORT:tenants:core,other');
      expect(() => providePlatform([coreModule, withPorts('other', { tenantSelection: Selection })])).toThrow('PLATFORM_DUPLICATE_PORT:tenantSelection:core,other');
    });

    it('applies the port check to platformRoutes too', () => {
      expect(() => platformRoutes([withPorts('core', { access: AccessAndFlags }), withPorts('other', { access: OtherAccess })])).toThrow('PLATFORM_DUPLICATE_PORT:access:core,other');
    });

    it('lets two modules bind different ports', () => {
      expect(() => providePlatform([withPorts('core', { access: AccessAndFlags }), withPorts('other', { tenants: Directory })])).not.toThrow();
    });

    it('checks core first, then duplicate modules, then ports, then topbar items, then nav requirements', () => {
      const badNav = moduleWith('bad', [], [], { nav: [{ id: 'x', labelKey: 'nav.x', icon: 'i', path: '/x', requires: { orTenantlessOperator: true } }] });

      const topbar = [topbarItem('same', 'account')];

      const first = withPorts('core', { access: AccessAndFlags });

      expect(() => providePlatform([withPorts('a', { access: AccessAndFlags }), first])).toThrow('PLATFORM_CORE_MODULE_FIRST');
      expect(() => providePlatform([first, first])).toThrow('PLATFORM_DUPLICATE_MODULE:core');
      expect(() => providePlatform([first, { ...badNav, ports: { access: OtherAccess }, topbar }, moduleWith('c', [], [], { topbar })])).toThrow('PLATFORM_DUPLICATE_PORT:access:core,bad');
      expect(() => providePlatform([moduleWith('core'), { ...badNav, topbar }, moduleWith('c', [], [], { topbar })])).toThrow('PLATFORM_DUPLICATE_TOPBAR_ITEM:same');
      expect(() => providePlatform([moduleWith('core'), badNav])).toThrow('PLATFORM_INVALID_REQUIREMENT:bad/x');
    });

    it('binds the four ports of coreModule to the core classes', () => {
      configureFeatureTest([], [{ provide: MessageService, useValue: { add: vi.fn() } }]);
      TestBed.configureTestingModule({ providers: [providePlatform([coreModule])] });

      expect(TestBed.inject(ACCESS_PROVIDER)).toBeInstanceOf(SessionAccess);
      expect(TestBed.inject(FLAG_SOURCE)).toBe(TestBed.inject(ACCESS_PROVIDER));
      expect(TestBed.inject(TENANT_DIRECTORY)).toBeInstanceOf(TenantsApi);
      expect(TestBed.inject(TENANT_SELECTION)).toBeInstanceOf(TenancySelection);
      expect(TestBed.inject(TENANT_SELECTION)?.noTenantPath).toBe('/no-tenant');
    });

    it('falls back to OPEN_ACCESS, NO_FLAGS, null and null when no module binds a port', () => {
      TestBed.configureTestingModule({ providers: [providePlatform([moduleWith('core')])] });

      expect(TestBed.inject(ACCESS_PROVIDER)).toBe(TestBed.inject(OPEN_ACCESS));
      expect(TestBed.inject(FLAG_SOURCE)).toBe(NO_FLAGS);
      expect(TestBed.inject(TENANT_DIRECTORY)).toBeNull();
      expect(TestBed.inject(TENANT_SELECTION)).toBeNull();
    });

    it('resolves the same four defaults without providePlatform', () => {
      TestBed.configureTestingModule({});

      expect(TestBed.inject(ACCESS_PROVIDER)).toBe(TestBed.inject(OPEN_ACCESS));
      expect(TestBed.inject(FLAG_SOURCE)).toBe(NO_FLAGS);
      expect(TestBed.inject(TENANT_DIRECTORY)).toBeNull();
      expect(TestBed.inject(TENANT_SELECTION)).toBeNull();
    });

    it('binds the classes a module sets, and access and flags of one class resolve to one instance', () => {
      TestBed.configureTestingModule({
        providers: [providePlatform([withPorts('core', { access: AccessAndFlags, flags: AccessAndFlags, tenants: Directory, tenantSelection: Selection })])]
      });

      expect(TestBed.inject(ACCESS_PROVIDER)).toBeInstanceOf(AccessAndFlags);
      expect(TestBed.inject(FLAG_SOURCE)).toBe(TestBed.inject(ACCESS_PROVIDER));
      expect(TestBed.inject(TENANT_DIRECTORY)).toBeInstanceOf(Directory);
      expect(TestBed.inject(TENANT_SELECTION)?.noTenantPath).toBe('/pick-tenant');
    });

    it('binds one port and leaves the others on their defaults', () => {
      TestBed.configureTestingModule({ providers: [providePlatform([withPorts('core', { tenantSelection: Selection })])] });

      expect(TestBed.inject(TENANT_SELECTION)).toBeInstanceOf(Selection);
      expect(TestBed.inject(ACCESS_PROVIDER)).toBe(TestBed.inject(OPEN_ACCESS));
      expect(TestBed.inject(FLAG_SOURCE)).toBe(NO_FLAGS);
      expect(TestBed.inject(TENANT_DIRECTORY)).toBeNull();
    });
  });

  describe('topbar and interceptors', () => {
    it('throws PLATFORM_DUPLICATE_TOPBAR_ITEM when an id repeats across modules', () => {
      const first = moduleWith('core', [], [], { topbar: [topbarItem('menu', 'account')] });

      const second = moduleWith('other', [], [], { topbar: [topbarItem('menu', 'context')] });

      expect(() => providePlatform([first, second])).toThrow('PLATFORM_DUPLICATE_TOPBAR_ITEM:menu');
    });

    it('throws PLATFORM_DUPLICATE_TOPBAR_ITEM when an id repeats inside one module', () => {
      const only = moduleWith('core', [], [], { topbar: [topbarItem('menu', 'account'), topbarItem('menu', 'context')] });

      expect(() => platformRoutes([only])).toThrow('PLATFORM_DUPLICATE_TOPBAR_ITEM:menu');
    });

    it('lists the topbar items of a slot in module order, then array order', () => {
      const first = moduleWith('core', [], [], { topbar: [topbarItem('a1', 'account'), topbarItem('c1', 'context'), topbarItem('c2', 'context')] });

      const second = moduleWith('other', [], [], { topbar: [topbarItem('c3', 'context'), topbarItem('a2', 'account')] });

      const modules = [first, second];

      expect(platformTopbar(modules, 'context').map((item) => item.id)).toEqual(['c1', 'c2', 'c3']);
      expect(platformTopbar(modules, 'account').map((item) => item.id)).toEqual(['a1', 'a2']);
    });

    it('contributes nothing for a module without topbar items', () => {
      expect(platformTopbar([moduleWith('core')], 'context')).toEqual([]);
    });

    it('lists interceptors in module order, then array order', () => {
      const first = moduleWith('core', [], [], { interceptors: [traceA, traceB] });

      const second = moduleWith('other', [], [], { interceptors: [traceC] });

      expect(platformInterceptors([first, moduleWith('empty'), second])).toEqual([traceA, traceB, traceC]);
      expect(platformInterceptors([moduleWith('core')])).toEqual([]);
    });
  });

  describe('nav requirements', () => {
    it('throws PLATFORM_INVALID_REQUIREMENT with the module and entry ids', () => {
      const bad = moduleWith('parameters', [], [], {
        nav: [
          { id: 'ok', labelKey: 'nav.ok', icon: 'i', path: '/ok', requires: { roles: ['admin'], orTenantlessOperator: true } },
          { id: 'both', labelKey: 'nav.both', icon: 'i', path: '/both', requires: { roles: ['admin'], platformOperator: true, orTenantlessOperator: true } }
        ]
      });

      expect(() => providePlatform([moduleWith('core'), bad])).toThrow('PLATFORM_INVALID_REQUIREMENT:parameters/both');
    });

    it('accepts the nav of coreModule', () => {
      expect(() => providePlatform([coreModule])).not.toThrow();
    });
  });

  describe('navLabel', () => {
    it('reads the label through the scope of the module of the entry', async () => {
      configureFeatureTest();

      const entry = coreModule.nav.find((candidate) => candidate.id === 'memberships');

      expect(entry).toBeDefined();
      expect(await firstValueFrom(navLabel(TestBed.inject(TranslocoService), coreModule, entry!))).toBe('Miembros');
    });
  });
});
