import { InjectionToken } from '@angular/core';

/**
 * Presentation of a topbar's account slot: `true` in a topbar that shows the
 * slot behind an overlay trigger, absent in one that renders it inline. The
 * shell frame picks it; account components read it instead of reading a flag.
 */
export const ACCOUNT_SLOT_OVERLAY = new InjectionToken<boolean>('ACCOUNT_SLOT_OVERLAY');
