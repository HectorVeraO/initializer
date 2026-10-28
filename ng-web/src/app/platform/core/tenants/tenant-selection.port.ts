import { InjectionToken } from '@angular/core';

/** Present only in a multi-tenant console: where to send a visitor who needs an active tenant. */
export interface TenantSelection {
  /** Shell path of the page that lets the visitor get a tenant. */
  readonly noTenantPath: string;
}

/** Null: single-tenant console. */
export const TENANT_SELECTION = new InjectionToken<TenantSelection | null>('TENANT_SELECTION', { providedIn: 'root', factory: () => null });
