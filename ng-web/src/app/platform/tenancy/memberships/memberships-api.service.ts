import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';
import type { MembershipStatus, TenantRef, TenantRole } from '@/platform/core';

/** One member of the active tenant as the API lists it (identity `MemberView`): a membership item plus the account. */
export interface MemberView {
  readonly id: string;
  readonly tenant: TenantRef;
  readonly role: TenantRole;
  readonly status: MembershipStatus;
  readonly account: { readonly id: string; readonly email: string; readonly fullName: string | null };
  readonly createdAt: string;
}

/** The one field a PATCH changes. */
export type MembershipChange = { readonly role: TenantRole } | { readonly status: MembershipStatus };

/** The contract maximum page size of identity lists. */
export const MEMBERSHIPS_PAGE_SIZE = 100;

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** Calls of the membership routes of identity. */
@Injectable({ providedIn: 'root' })
export class MembershipsApi {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/memberships?page=1&size=100&sort=createdAt (active and suspended). The view shows its own load error. */
  list(): Observable<ApiPage<MemberView>> {
    const params = buildParams({ page: 1, size: MEMBERSHIPS_PAGE_SIZE, sort: 'createdAt' });

    const page = this.http.get<ApiEnvelope<ApiPage<MemberView>>>(this.runtimeConfig.apiPath('/api/v1/memberships'), { params, context: skipErrorToast() }).pipe(unwrapData<ApiPage<MemberView>>());

    return page;
  }

  /** PATCH /api/v1/memberships/{id} with exactly the given field. The view shows its own failure message. */
  update(id: string, change: MembershipChange): Observable<MemberView> {
    const body = 'role' in change ? { role: change.role } : { status: change.status };

    const updated = this.http.patch<ApiEnvelope<MemberView>>(this.runtimeConfig.apiPath(`/api/v1/memberships/${pathSegment(id)}`), body, { context: skipErrorToast() }).pipe(unwrapData<MemberView>());

    return updated;
  }
}
