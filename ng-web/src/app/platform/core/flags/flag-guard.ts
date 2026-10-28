import { inject } from '@angular/core';
import type { CanMatchFn } from '@angular/router';
import { FLAG_SOURCE } from './flag-source.port';

/** True when the bound flag source enables `flagKey`; an unknown key, or no flag source, is false. */
export function flagGuard(flagKey: string): CanMatchFn {
  return () => inject(FLAG_SOURCE).isEnabled(flagKey);
}
