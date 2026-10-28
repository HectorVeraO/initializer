import type { HttpInterceptorFn } from '@angular/common/http';
import { EnvironmentProviders, InjectionToken, Provider, Type, makeEnvironmentProviders } from '@angular/core';
import type { Routes } from '@angular/router';
import type { ProviderScope, TranslocoService } from '@jsverse/transloco';
import type { Observable } from 'rxjs';
import { ACCESS_PROVIDER, OPEN_ACCESS } from '../access/access.port';
import type { AccessProvider } from '../access/access.port';
import type { AccessRequirement } from '../access/access.types';
import { assertValidRequirement } from '../access/require-access';
import { FLAG_SOURCE, NO_FLAGS } from '../flags/flag-source.port';
import type { FlagSource } from '../flags/flag-source.port';
import { TENANT_DIRECTORY } from '../tenants/tenant-directory.port';
import type { TenantDirectory } from '../tenants/tenant-directory.port';
import { TENANT_SELECTION } from '../tenants/tenant-selection.port';
import type { TenantSelection } from '../tenants/tenant-selection.port';

/** One catalog module as the console shell sees it. Pure data: no side effects at import. */
export interface WebModule {
  /** Catalog module id, equal to module.manifest.json `module`. */
  readonly id: string;
  /** Children of the shell group under AppLayout; each lazy route carries its own requireAccess guard. */
  readonly shellRoutes: Routes;
  /** Top-level routes outside AppLayout, rendered in AppPublicLayout. */
  readonly publicRoutes: Routes;
  /** Menu section of this module; empty for none. */
  readonly nav: readonly NavEntry[];
  /** Transloco scope that holds `nav[].labelKey` and `menu.title`. */
  readonly i18n: ProviderScope;
  /** Topbar components, rendered by slot. */
  readonly topbar?: readonly TopbarItem[];
  /** Interceptors run before the core ones (trace, API error), in module order then array order. */
  readonly interceptors?: readonly HttpInterceptorFn[];
  /** Implementations of the shell's singleton ports. At most one installed module may set each. */
  readonly ports?: PortBindings;
}

export interface NavEntry {
  /** Stable id, also the menu item's `data-testid` suffix: `nav-<id>`. */
  readonly id: string;
  /** Key relative to the module's scope, e.g. 'nav.memberships'. */
  readonly labelKey: string;
  /** Sakai menu icon class, e.g. 'menu-icon menu-icon-users'. */
  readonly icon: string;
  readonly path: string;
  /** Shown only when allowsAccess(requires) holds. `{}`: any visitor the installed provider admits. */
  readonly requires: AccessRequirement;
}

export interface TopbarItem {
  /** Unique across modules; the wrapper's `data-testid` is `topbar-<id>`. */
  readonly id: string;
  /** 'context' items render before 'account' items; module order, then array order, within a slot. */
  readonly slot: 'context' | 'account';
  /** Standalone component with no required inputs; it decides on its own whether to render anything. */
  readonly component: Type<unknown>;
}

/** Each bound class must be `@Injectable({ providedIn: 'root' })`; the token is bound with `useExisting`. */
export interface PortBindings {
  readonly access?: Type<AccessProvider>;
  readonly flags?: Type<FlagSource>;
  readonly tenants?: Type<TenantDirectory>;
  readonly tenantSelection?: Type<TenantSelection>;
}

type PortName = keyof PortBindings;

/** Checked in this order, which is also the order a duplicate is reported in. */
const PORT_NAMES: readonly PortName[] = ['access', 'flags', 'tenants', 'tenantSelection'];

/** Multi-module token read by the menu and the topbar. */
export const PLATFORM_MODULES = new InjectionToken<readonly WebModule[]>('PLATFORM_MODULES');

function assertValidModules(modules: readonly WebModule[]): void {
  if (modules[0]?.id !== 'core') {
    throw new Error('PLATFORM_CORE_MODULE_FIRST');
  }

  const seen = new Set<string>();

  for (const module of modules) {
    if (seen.has(module.id)) {
      throw new Error(`PLATFORM_DUPLICATE_MODULE:${module.id}`);
    }

    seen.add(module.id);
  }

  assertSinglePortBindings(modules);
  assertUniqueTopbarItems(modules);
  assertValidNavRequirements(modules);
}

function assertSinglePortBindings(modules: readonly WebModule[]): void {
  for (const port of PORT_NAMES) {
    const binders = modules.filter((module) => module.ports?.[port] !== undefined).map((module) => module.id);

    if (binders.length > 1) {
      throw new Error(`PLATFORM_DUPLICATE_PORT:${port}:${binders[0]},${binders[1]}`);
    }
  }
}

function assertUniqueTopbarItems(modules: readonly WebModule[]): void {
  const seen = new Set<string>();

  for (const module of modules) {
    for (const item of module.topbar ?? []) {
      if (seen.has(item.id)) {
        throw new Error(`PLATFORM_DUPLICATE_TOPBAR_ITEM:${item.id}`);
      }

      seen.add(item.id);
    }
  }
}

function assertValidNavRequirements(modules: readonly WebModule[]): void {
  for (const module of modules) {
    for (const entry of module.nav) {
      try {
        assertValidRequirement(entry.requires);
      } catch {
        throw new Error(`PLATFORM_INVALID_REQUIREMENT:${module.id}/${entry.id}`);
      }
    }
  }
}

/** Binds `token` to the class a module sets for `port` with `useExisting`, else to `fallback`. */
function portProvider<T>(modules: readonly WebModule[], port: PortName, token: InjectionToken<T>, fallback: Provider): Provider {
  const bound = modules.map((module) => module.ports?.[port]).find((type) => type !== undefined);

  const provider: Provider = bound === undefined ? fallback : { provide: token, useExisting: bound };

  return provider;
}

/**
 * Providers for app.config.ts: PLATFORM_MODULES, and ACCESS_PROVIDER, FLAG_SOURCE, TENANT_DIRECTORY, TENANT_SELECTION
 * bound with `useExisting` to the class a module sets, else OPEN_ACCESS, NO_FLAGS, null, null. Registers no Transloco scope.
 * Throws, in this order of checks: Error('PLATFORM_CORE_MODULE_FIRST'), Error('PLATFORM_DUPLICATE_MODULE:<id>'),
 * Error('PLATFORM_DUPLICATE_PORT:<port>:<id>,<id>'), Error('PLATFORM_DUPLICATE_TOPBAR_ITEM:<id>'),
 * Error('PLATFORM_INVALID_REQUIREMENT:<moduleId>/<navId>').
 */
export function providePlatform(modules: readonly WebModule[]): EnvironmentProviders {
  assertValidModules(modules);

  const providers = makeEnvironmentProviders([
    { provide: PLATFORM_MODULES, useValue: modules },
    portProvider(modules, 'access', ACCESS_PROVIDER, { provide: ACCESS_PROVIDER, useExisting: OPEN_ACCESS }),
    portProvider(modules, 'flags', FLAG_SOURCE, { provide: FLAG_SOURCE, useValue: NO_FLAGS }),
    portProvider(modules, 'tenants', TENANT_DIRECTORY, { provide: TENANT_DIRECTORY, useValue: null }),
    portProvider(modules, 'tenantSelection', TENANT_SELECTION, { provide: TENANT_SELECTION, useValue: null })
  ]);

  return providers;
}

/** Route sets of `modules` in order (core first). Same checks as providePlatform. */
export function platformRoutes(modules: readonly WebModule[]): { readonly shell: Routes; readonly public: Routes } {
  assertValidModules(modules);

  const routes = {
    shell: modules.flatMap((module) => module.shellRoutes),
    public: modules.flatMap((module) => module.publicRoutes)
  };

  return routes;
}

/** Every module's interceptors in module order, then array order, for provideApiHttpClient(outer). */
export function platformInterceptors(modules: readonly WebModule[]): HttpInterceptorFn[] {
  const interceptors = modules.flatMap((module) => module.interceptors ?? []);

  return interceptors;
}

/** Topbar items of one slot: module order, then array order. */
export function platformTopbar(modules: readonly WebModule[], slot: TopbarItem['slot']): readonly TopbarItem[] {
  const items = modules.flatMap((module) => module.topbar ?? []).filter((item) => item.slot === slot);

  return items;
}

/** The label of `entry`, read through its module's Transloco scope; follows the active language. */
export function navLabel(transloco: TranslocoService, module: WebModule, entry: NavEntry): Observable<string> {
  const label = transloco.selectTranslate(entry.labelKey, {}, module.i18n);

  return label;
}
