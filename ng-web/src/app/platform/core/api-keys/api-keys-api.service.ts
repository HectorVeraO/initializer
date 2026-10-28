import { HttpClient, HttpContext } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { buildParams } from '@/core/api/api-params';
import { pathSegment } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { unwrapData } from '@/core/envelope';
import type { ApiEnvelope, ApiPage } from '@/core/envelope';

export type ApiKeyStatus = 'active' | 'expired' | 'revoked';

/** An API key as listed: the prefix identifies it, the secret is never part of it. */
export interface ApiKeyView {
  readonly id: string;
  readonly name: string;
  readonly keyPrefix: string;
  readonly scopes: readonly string[];
  readonly status: ApiKeyStatus;
  readonly expiresAt: string | null;
  readonly revokedAt: string | null;
  readonly createdAt: string;
}

/** A scope an API key can be granted. */
export interface ApiKeyScope {
  readonly name: string;
  readonly description: string;
}

/** `data` of POST /api/v1/api-keys. `plainTextKey` is shown once and kept nowhere. */
export interface CreatedApiKey {
  readonly apiKey: ApiKeyView;
  readonly plainTextKey: string;
}

export interface CreateApiKeyInput {
  readonly name: string;
  readonly scopes: readonly string[];
  readonly expiresAt: string | null;
}

/** The contract maximum page size of identity lists. */
export const API_KEYS_PAGE_SIZE = 100;

function skipErrorToast(): HttpContext {
  return new HttpContext().set(SKIP_ERROR_TOAST, true);
}

/** Calls of the API key routes of identity. */
@Injectable({ providedIn: 'root' })
export class ApiKeysApi {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);

  /** GET /api/v1/api-keys?page=1&size=100&sort=-createdAt. The view shows its own load error. */
  list(): Observable<ApiPage<ApiKeyView>> {
    const params = buildParams({ page: 1, size: API_KEYS_PAGE_SIZE, sort: '-createdAt' });

    const page = this.http.get<ApiEnvelope<ApiPage<ApiKeyView>>>(this.runtimeConfig.apiPath('/api/v1/api-keys'), { params, context: skipErrorToast() }).pipe(unwrapData<ApiPage<ApiKeyView>>());

    return page;
  }

  /** GET /api/v1/api-key-scopes: the scopes the installed modules register. */
  scopes(): Observable<readonly ApiKeyScope[]> {
    const scopes = this.http.get<ApiEnvelope<readonly ApiKeyScope[]>>(this.runtimeConfig.apiPath('/api/v1/api-key-scopes'), { context: skipErrorToast() }).pipe(unwrapData<readonly ApiKeyScope[]>());

    return scopes;
  }

  /** POST /api/v1/api-keys -> 201. */
  create(input: CreateApiKeyInput): Observable<CreatedApiKey> {
    const body = { name: input.name, scopes: input.scopes, expiresAt: input.expiresAt };

    const created = this.http.post<ApiEnvelope<CreatedApiKey>>(this.runtimeConfig.apiPath('/api/v1/api-keys'), body, { context: skipErrorToast() }).pipe(unwrapData<CreatedApiKey>());

    return created;
  }

  /** DELETE /api/v1/api-keys/{id} -> 200. Revoking a revoked key answers 200 again. */
  revoke(id: string): Observable<ApiKeyView> {
    const revoked = this.http.delete<ApiEnvelope<ApiKeyView>>(this.runtimeConfig.apiPath(`/api/v1/api-keys/${pathSegment(id)}`), { context: skipErrorToast() }).pipe(unwrapData<ApiKeyView>());

    return revoked;
  }
}
