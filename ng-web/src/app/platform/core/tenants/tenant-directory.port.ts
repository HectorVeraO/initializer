import { InjectionToken } from '@angular/core';
import type { Observable } from 'rxjs';
import type { ApiPage } from '@/core/envelope';
import type { TenantRef } from '../access/access.types';

/** Operator-wide tenant list for screens that pick a tenant. */
export interface TenantDirectory {
  /** First page at the contract maximum size, newest first. */
  list(): Observable<ApiPage<TenantRef>>;
}

/** Null when no installed module provides tenants. */
export const TENANT_DIRECTORY = new InjectionToken<TenantDirectory | null>('TENANT_DIRECTORY', { providedIn: 'root', factory: () => null });
