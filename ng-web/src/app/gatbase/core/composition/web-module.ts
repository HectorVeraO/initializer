import { EnvironmentProviders, InjectionToken, makeEnvironmentProviders } from '@angular/core';
import type { Routes } from '@angular/router';
import { provideTranslocoScope, type ProviderScope } from '@jsverse/transloco';
import type { SessionRequirement } from '../guards/requirement';
import { GATBASE_CORE_I18N } from '../module';

/** One catalog module as the console shell sees it. Pure data: no side effects at import. */
export interface GatbaseWebModule {
  /** Catalog module id, equal to module.manifest.json `module`. */
  readonly id: 'core' | 'parameters' | 'feature-flags' | string;
  /** Routes mounted as children of AppLayout (authenticated shell). Each entry is lazy and carries its own requireSession guard. */
  readonly shellRoutes: Routes;
  /** Routes mounted at top level, outside AppLayout. Only core declares any. */
  readonly publicRoutes: Routes;
  /** Menu section of this module; empty for none. */
  readonly nav: readonly NavEntry[];
  /** Transloco scope that holds `nav[].labelKey`. */
  readonly i18n: ProviderScope;
}

export interface NavEntry {
  /** Stable id, also the menu item's `data-testid` suffix: `nav-<id>`. */
  readonly id: string;
  /** Key relative to the module's scope, e.g. 'nav.memberships'. */
  readonly labelKey: string;
  /** Sakai menu icon class, e.g. 'menu-icon menu-icon-users'. */
  readonly icon: string;
  readonly path: string;
  readonly requires: SessionRequirement;
}

/** Multi-module token read by the menu. */
export const GATBASE_MODULES = new InjectionToken<readonly GatbaseWebModule[]>('GATBASE_MODULES');

function assertValidModules(modules: readonly GatbaseWebModule[]): void {
  if (modules[0]?.id !== 'core') {
    throw new Error('GATBASE_CORE_MODULE_FIRST');
  }

  const seen = new Set<string>();

  for (const module of modules) {
    if (seen.has(module.id)) {
      throw new Error(`GATBASE_DUPLICATE_MODULE:${module.id}`);
    }

    seen.add(module.id);
  }
}

/** Providers for app.config.ts: GATBASE_MODULES = modules and the core Transloco scope. Throws Error('GATBASE_CORE_MODULE_FIRST') unless modules[0].id === 'core', Error('GATBASE_DUPLICATE_MODULE:<id>') when an id repeats. */
export function provideGatbase(modules: readonly GatbaseWebModule[]): EnvironmentProviders {
  assertValidModules(modules);

  const providers = makeEnvironmentProviders([{ provide: GATBASE_MODULES, useValue: modules }, provideTranslocoScope(GATBASE_CORE_I18N)]);

  return providers;
}

/** Route sets of `modules` in order (core first). Same checks as provideGatbase. */
export function gatbaseRoutes(modules: readonly GatbaseWebModule[]): { readonly shell: Routes; readonly public: Routes } {
  assertValidModules(modules);

  const routes = {
    shell: modules.flatMap((module) => module.shellRoutes),
    public: modules.flatMap((module) => module.publicRoutes)
  };

  return routes;
}
