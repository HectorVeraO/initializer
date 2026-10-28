import { Injectable } from '@angular/core';
import type { TenantSelection } from '@/platform/core';

/** Binds `TENANT_SELECTION`: a visitor who needs an active tenant is sent to the no-tenant page. */
@Injectable({ providedIn: 'root' })
export class TenancySelection implements TenantSelection {
  readonly noTenantPath = '/no-tenant';
}
