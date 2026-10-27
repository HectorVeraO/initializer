import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';
import type { MembershipRole, SessionMembership } from '../session/session.types';

/** One invitation as the API lists it (`InvitationView`). The token never appears here. */
export interface InvitationView {
  readonly id: string;
  readonly email: string;
  readonly role: MembershipRole;
  readonly status: 'pending' | 'accepted' | 'revoked' | 'expired';
  readonly expiresAt: string;
  readonly createdAt: string;
  readonly acceptedAt: string | null;
  readonly revokedAt: string | null;
}

/** `data` of POST /api/v1/invitations. `invitationUrl` is shown once and kept nowhere. */
export interface CreatedInvitation {
  readonly invitation: InvitationView;
  readonly invitationUrl: string;
}

export interface AcceptInvitationResult {
  readonly membership: SessionMembership;
}

export interface AcceptNewInvitationResult extends AcceptInvitationResult {
  readonly account: { readonly id: string; readonly email: string; readonly fullName: string | null };
}

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** The contract maximum page size of core-auth lists. */
export const INVITATIONS_PAGE_SIZE = 100;

/** Calls of the invitation routes of core-auth. */
@Injectable({ providedIn: 'root' })
export class InvitationsApi {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/invitations?status=pending&page=1&size=100&sort=-createdAt. The view shows its own load error. */
  listPending(): Observable<ApiPage<InvitationView>> {
    const params = buildParams({ status: 'pending', page: 1, size: INVITATIONS_PAGE_SIZE, sort: '-createdAt' });

    const context = skipErrorToast();

    const page = this.http.get<ApiEnvelope<ApiPage<InvitationView>>>(this.runtimeConfig.apiPath('/api/v1/invitations'), { params, context }).pipe(unwrapData<ApiPage<InvitationView>>());

    return page;
  }

  /** POST /api/v1/invitations -> 201. */
  create(input: { readonly email: string; readonly role: MembershipRole }): Observable<CreatedInvitation> {
    const created = this.http.post<ApiEnvelope<CreatedInvitation>>(this.runtimeConfig.apiPath('/api/v1/invitations'), { email: input.email, role: input.role }, { context: skipErrorToast() }).pipe(unwrapData<CreatedInvitation>());

    return created;
  }

  /** DELETE /api/v1/invitations/{id} -> 200. */
  revoke(id: string): Observable<InvitationView> {
    const revoked = this.http.delete<ApiEnvelope<InvitationView>>(this.runtimeConfig.apiPath(`/api/v1/invitations/${pathSegment(id)}`)).pipe(unwrapData<InvitationView>());

    return revoked;
  }

  /** POST /api/v1/invitations/accept (session). */
  accept(token: string): Observable<AcceptInvitationResult> {
    const accepted = this.http.post<ApiEnvelope<AcceptInvitationResult>>(this.runtimeConfig.apiPath('/api/v1/invitations/accept'), { token }, { context: skipErrorToast() }).pipe(unwrapData<AcceptInvitationResult>());

    return accepted;
  }

  /** POST /api/v1/invitations/accept-new (anonymous). */
  acceptNew(input: { readonly token: string; readonly password: string; readonly fullName: string }): Observable<AcceptNewInvitationResult> {
    const body = { token: input.token, password: input.password, fullName: input.fullName };

    const accepted = this.http.post<ApiEnvelope<AcceptNewInvitationResult>>(this.runtimeConfig.apiPath('/api/v1/invitations/accept-new'), body, { context: skipErrorToast() }).pipe(unwrapData<AcceptNewInvitationResult>());

    return accepted;
  }
}
