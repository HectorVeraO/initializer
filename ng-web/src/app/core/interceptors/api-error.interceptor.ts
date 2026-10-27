import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { catchError, throwError } from 'rxjs';
import { isForbiddenPageError, withTraceReference } from '@/core/api/api-error';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';

/**
 * Turns failures of calls to this deployment's API into visible behaviour: the
 * forbidden page on a `GET`/`HEAD` `403` whose code is in `FORBIDDEN_PAGE_CODES`, a generic toast on a network or
 * server failure. Calls to other hosts are left alone.
 */
export const apiErrorInterceptor: HttpInterceptorFn = (req, next) => {
  const runtimeConfig = inject(RuntimeConfigService);

  const router = inject(Router);

  const messages = inject(MessageService);

  const transloco = inject(TranslocoService);

  return next(req).pipe(
    catchError((error: unknown) => {
      if (!(error instanceof HttpErrorResponse) || !runtimeConfig.isApiUrl(req.url)) {
        return throwError(() => error);
      }

      const isRead = req.method === 'GET' || req.method === 'HEAD';

      if (isRead && isForbiddenPageError(error)) {
        void router.navigate(['/forbidden']);
      } else if ((error.status === 0 || error.status >= 500) && !req.context.get(SKIP_ERROR_TOAST)) {
        messages.add({
          severity: 'error',
          summary: transloco.translate('errors.generic-error.summary'),
          detail: withTraceReference(transloco, transloco.translate('errors.operationFailed'), error)
        });
      }

      return throwError(() => error);
    })
  );
};
