import type { WebModule } from './platform/core';

/**
 * The console modules this project registers itself.
 *
 * This file is project-owned: product verticals add themselves here, after the catalog modules that
 * `platform/modules.ts` installs. `platform/modules.ts` is generator-owned and must not be edited.
 */
export const productModules: readonly WebModule[] = [];
