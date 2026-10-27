import { HttpContext, HttpContextToken, HttpErrorResponse, HttpEvent, HttpInterceptorFn, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { Observable, catchError, from, switchMap, throwError } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorCode } from '@/core/api/api-error';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { notifyTenantChanged } from './session-notice';
import { NO_TENANT_HEADER, SESSION_LIFECYCLE, SessionStore } from './session-store.service';

/** Set on the retried request so a second 401 surfaces instead of looping. */
export const SKIP_SESSION_RETRY = new HttpContextToken<boolean>(() => false);

const TENANT_HEADER = 'X-Active-Tenant-Id';

/** Copy of the request's context with `SKIP_SESSION_RETRY` set; the caller's context object is left untouched. */
function retryContext(context: HttpContext): HttpContext {
  const copy = new HttpContext();

  for (const token of context.keys()) {
    copy.set(token, context.get(token));
  }

  copy.set(SKIP_SESSION_RETRY, true);

  return copy;
}

/** The URL without its fragment: a fragment can carry a one-time secret and must never reach a query parameter. */
function withoutFragment(url: string): string {
  const hashAt = url.indexOf('#');

  return hashAt === -1 ? url : url.slice(0, hashAt);
}

/**
 * Session behaviour of every call to this deployment's API: the stale-tab header, one
 * refresh-and-retry on `401 SESSION_INVALID`, and the navigation for `409 ACTIVE_TENANT_CHANGED`
 * and `403 ACTIVE_TENANT_REQUIRED`. Registered outermost so a retry gets a fresh `traceparent`.
 */
export const gatbaseSessionInterceptor: HttpInterceptorFn = (req, next) => {
  const runtimeConfig = inject(RuntimeConfigService);

  if (!runtimeConfig.isApiUrl(req.url)) {
    return next(req);
  }

  const store = inject(SessionStore);

  const router = inject(Router);

  const messages = inject(MessageService);

  const transloco = inject(TranslocoService);

  const send = (request: HttpRequest<unknown>): Observable<HttpEvent<unknown>> => {
    const isLifecycle = request.context.get(SESSION_LIFECYCLE);

    const tenantId = store.activeTenantId();

    const addsHeader = !isLifecycle && !request.context.get(NO_TENANT_HEADER) && tenantId !== null && !request.headers.has(TENANT_HEADER);

    const outgoing = addsHeader ? request.clone({ setHeaders: { [TENANT_HEADER]: tenantId } }) : request;

    return next(outgoing).pipe(
      catchError((error: unknown) => {
        if (!(error instanceof HttpErrorResponse)) {
          return throwError(() => error);
        }

        const code = errorCode(error);

        const canRefresh = error.status === 401 && code === ApiErrorCode.SessionInvalid && !isLifecycle && !request.context.get(SKIP_SESSION_RETRY);

        if (canRefresh) {
          return from(store.refresh().catch(() => 'failed' as const)).pipe(
            switchMap((outcome) => {
              if (outcome === 'refreshed') {
                // The retry keeps the header the original carried, so a tenant that changed in between is refused with 409, not silently followed.
                return send(outgoing.clone({ context: retryContext(outgoing.context) }));
              }

              if (outcome === 'anonymous') {
                void router.navigate(['/login'], { queryParams: { returnUrl: withoutFragment(router.url) } });
              } else if (outcome === 'denied') {
                void router.navigate(['/home']);
              }

              return throwError(() => error);
            })
          );
        }

        if (error.status === 409 && code === ApiErrorCode.ActiveTenantChanged) {
          const handled = (async () => {
            await notifyTenantChanged(messages, transloco);
            await store.reload().catch(() => undefined);
            await router.navigate(['/home']);
          })();

          return from(handled).pipe(switchMap(() => throwError(() => error)));
        }

        if (error.status === 403 && code === ApiErrorCode.ActiveTenantRequired && request.method === 'GET') {
          void router.navigate(['/no-tenant']);
        }

        return throwError(() => error);
      })
    );
  };

  return send(req);
};
