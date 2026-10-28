import { InjectionToken } from '@angular/core';

/** Where the shell reads feature flags from. */
export interface FlagSource {
  /** Default-closed: true only for a key the source knows as enabled; an unknown key is false. */
  isEnabled(flagKey: string): boolean;
}

/** Bound when no module sets `ports.flags`: every key is false. */
export const NO_FLAGS: FlagSource = {
  isEnabled: () => false
};

export const FLAG_SOURCE = new InjectionToken<FlagSource>('FLAG_SOURCE', { providedIn: 'root', factory: () => NO_FLAGS });
