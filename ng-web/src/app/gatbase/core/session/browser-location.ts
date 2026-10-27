import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

/** The part of `window.location` the session code uses; specs replace it because jsdom cannot navigate. */
export type BrowserLocation = Pick<Location, 'assign' | 'origin'>;

export const BROWSER_LOCATION = new InjectionToken<BrowserLocation>('GATBASE_BROWSER_LOCATION', {
  providedIn: 'root',
  factory: () => inject(DOCUMENT).location
});
