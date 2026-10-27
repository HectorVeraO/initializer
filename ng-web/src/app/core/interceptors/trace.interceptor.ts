import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { environment } from '../../../environments/environment';

/** Random lowercase hex string of `bytes` bytes. */
function randomHex(bytes: number): string {
  const values = crypto.getRandomValues(new Uint8Array(bytes));

  return Array.from(values, (value) => value.toString(16).padStart(2, '0')).join('');
}

function isSampled(): boolean {
  return Math.random() < environment.trace.samplingRate;
}

/** Opens a W3C `traceparent` context on every request to this deployment's API; other hosts get none. */
export const TraceInterceptor: HttpInterceptorFn = (req, next) => {
  const runtimeConfig = inject(RuntimeConfigService);

  if (!runtimeConfig.isApiUrl(req.url)) {
    return next(req);
  }

  const traceId = randomHex(16);

  const spanId = randomHex(8);

  const traceparent = `00-${traceId}-${spanId}-${isSampled() ? '01' : '00'}`;

  const tracedRequest = req.clone({ setHeaders: { traceparent } });

  return next(tracedRequest);
};
