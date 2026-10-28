import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';
import type { InvitationView, TenantStatus, TenantSummary } from '../session/session.types';
import type { TenantDirectory } from './tenant-directory.port';

/** A tenant as the platform operator lists it (identity `TenantView`). */
export interface TenantView extends TenantSummary {
  readonly createdAt: string;
}

/** `data` of POST /api/v1/tenants. `invitationUrl` is shown once and kept nowhere. */
export interface CreatedTenant {
  readonly tenant: TenantView;
  readonly invitation: InvitationView;
  readonly invitationUrl: string;
}

export interface CreateTenantInput {
  readonly slug: string;
  readonly name: string;
  readonly adminEmail: string;
}

/** The contract maximum page size of identity lists. */
export const TENANTS_PAGE_SIZE = 100;

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** Calls of the tenant routes of identity (platform operator). Binds `TENANT_DIRECTORY`: feature-flags reads the tenant list through it. */
@Injectable({ providedIn: 'root' })
export class TenantsApi implements TenantDirectory {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/tenants?page=1&size=100&sort=-createdAt. The view shows its own load error. */
  list(): Observable<ApiPage<TenantView>> {
    const params = buildParams({ page: 1, size: TENANTS_PAGE_SIZE, sort: '-createdAt' });

    const page = this.http.get<ApiEnvelope<ApiPage<TenantView>>>(this.runtimeConfig.apiPath('/api/v1/tenants'), { params, context: skipErrorToast() }).pipe(unwrapData<ApiPage<TenantView>>());

    return page;
  }

  /** POST /api/v1/tenants -> 201. */
  create(input: CreateTenantInput): Observable<CreatedTenant> {
    const body = { slug: input.slug, name: input.name, adminEmail: input.adminEmail };

    const created = this.http.post<ApiEnvelope<CreatedTenant>>(this.runtimeConfig.apiPath('/api/v1/tenants'), body, { context: skipErrorToast() }).pipe(unwrapData<CreatedTenant>());

    return created;
  }

  /** PATCH /api/v1/tenants/{id}/status -> 200. The view shows its own failure message. */
  setStatus(id: string, status: TenantStatus): Observable<TenantView> {
    const updated = this.http.patch<ApiEnvelope<TenantView>>(this.runtimeConfig.apiPath(`/api/v1/tenants/${pathSegment(id)}/status`), { status }, { context: skipErrorToast() }).pipe(unwrapData<TenantView>());

    return updated;
  }
}
