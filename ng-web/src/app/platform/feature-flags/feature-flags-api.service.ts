import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';
import type { EnabledBody, FlagView } from './feature-flags.types';

/** The contract maximum page size of the flag list. */
export const FLAGS_PAGE_SIZE = 100;

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** Calls of the operator routes of the feature-flags module. Every view shows its own errors. */
@Injectable({ providedIn: 'root' })
export class FeatureFlagsApi {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/feature-flags?page=1&size=100 (manifest order, no sort) */
  list(): Observable<ApiPage<FlagView>> {
    const params = buildParams({ page: 1, size: FLAGS_PAGE_SIZE });

    const page = this.http.get<ApiEnvelope<ApiPage<FlagView>>>(this.runtimeConfig.apiPath('/api/v1/feature-flags'), { params, context: skipErrorToast() }).pipe(unwrapData<ApiPage<FlagView>>());

    return page;
  }

  /** PUT /api/v1/feature-flags/{key} { enabled } -> 200 FlagView */
  setGlobal(key: string, enabled: boolean): Observable<FlagView> {
    const body: EnabledBody = { enabled };

    const updated = this.http.put<ApiEnvelope<FlagView>>(this.runtimeConfig.apiPath(`/api/v1/feature-flags/${pathSegment(key)}`), body, { context: skipErrorToast() }).pipe(unwrapData<FlagView>());

    return updated;
  }

  /** DELETE /api/v1/feature-flags/{key} -> 200 FlagView (idempotent; back to Inherit) */
  clearGlobal(key: string): Observable<FlagView> {
    const cleared = this.http.delete<ApiEnvelope<FlagView>>(this.runtimeConfig.apiPath(`/api/v1/feature-flags/${pathSegment(key)}`), { context: skipErrorToast() }).pipe(unwrapData<FlagView>());

    return cleared;
  }

  /** PUT /api/v1/feature-flags/{key}/tenants/{tenantId} { enabled } -> 200 FlagView */
  setTenant(key: string, tenantId: string, enabled: boolean): Observable<FlagView> {
    const body: EnabledBody = { enabled };

    const url = this.runtimeConfig.apiPath(`/api/v1/feature-flags/${pathSegment(key)}/tenants/${pathSegment(tenantId)}`);

    const updated = this.http.put<ApiEnvelope<FlagView>>(url, body, { context: skipErrorToast() }).pipe(unwrapData<FlagView>());

    return updated;
  }

  /** DELETE /api/v1/feature-flags/{key}/tenants/{tenantId} -> 200 FlagView (idempotent) */
  clearTenant(key: string, tenantId: string): Observable<FlagView> {
    const url = this.runtimeConfig.apiPath(`/api/v1/feature-flags/${pathSegment(key)}/tenants/${pathSegment(tenantId)}`);

    const cleared = this.http.delete<ApiEnvelope<FlagView>>(url, { context: skipErrorToast() }).pipe(unwrapData<FlagView>());

    return cleared;
  }
}
