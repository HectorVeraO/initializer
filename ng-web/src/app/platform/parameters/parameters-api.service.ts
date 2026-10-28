import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';
import type { ParameterCreate, ParameterDeleted, ParameterReplace, ParameterView } from './parameters.types';

/** The contract maximum page size of the parameters list. */
export const PARAMETERS_PAGE_SIZE = 100;

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** Calls of the routes of the parameters module. Every view shows its own errors. */
@Injectable({ providedIn: 'root' })
export class ParametersApi {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/parameters?page=1&size=100&sort=key */
  list(): Observable<ApiPage<ParameterView>> {
    const params = buildParams({ page: 1, size: PARAMETERS_PAGE_SIZE, sort: 'key' });

    const page = this.http.get<ApiEnvelope<ApiPage<ParameterView>>>(this.runtimeConfig.apiPath('/api/v1/parameters'), { params, context: skipErrorToast() }).pipe(unwrapData<ApiPage<ParameterView>>());

    return page;
  }

  /** POST /api/v1/parameters (operator) -> 201 */
  create(input: ParameterCreate): Observable<ParameterView> {
    const created = this.http.post<ApiEnvelope<ParameterView>>(this.runtimeConfig.apiPath('/api/v1/parameters'), input, { context: skipErrorToast() }).pipe(unwrapData<ParameterView>());

    return created;
  }

  /** PUT /api/v1/parameters/{key} (operator); the body carries no `key` -> 200 */
  update(key: string, input: ParameterReplace): Observable<ParameterView> {
    const updated = this.http.put<ApiEnvelope<ParameterView>>(this.runtimeConfig.apiPath(`/api/v1/parameters/${pathSegment(key)}`), input, { context: skipErrorToast() }).pipe(unwrapData<ParameterView>());

    return updated;
  }

  /** DELETE /api/v1/parameters/{key} (operator) -> 200 with the count of tenant overrides deleted with it */
  remove(key: string): Observable<ParameterDeleted> {
    const deleted = this.http.delete<ApiEnvelope<ParameterDeleted>>(this.runtimeConfig.apiPath(`/api/v1/parameters/${pathSegment(key)}`), { context: skipErrorToast() }).pipe(unwrapData<ParameterDeleted>());

    return deleted;
  }

  /** PUT /api/v1/parameters/{key}/tenant-override { value } (admin of the active tenant) -> 200 */
  setOverride(key: string, value: unknown): Observable<ParameterView> {
    const updated = this.http.put<ApiEnvelope<ParameterView>>(this.runtimeConfig.apiPath(`/api/v1/parameters/${pathSegment(key)}/tenant-override`), { value }, { context: skipErrorToast() }).pipe(unwrapData<ParameterView>());

    return updated;
  }

  /** DELETE /api/v1/parameters/{key}/tenant-override -> 200; clearing a missing override answers 200 again */
  clearOverride(key: string): Observable<ParameterView> {
    const cleared = this.http.delete<ApiEnvelope<ParameterView>>(this.runtimeConfig.apiPath(`/api/v1/parameters/${pathSegment(key)}/tenant-override`), { context: skipErrorToast() }).pipe(unwrapData<ParameterView>());

    return cleared;
  }
}
