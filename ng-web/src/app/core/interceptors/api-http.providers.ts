import { HttpInterceptorFn, provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import { TraceInterceptor } from '@/core/interceptors/trace.interceptor';
import { apiErrorInterceptor } from '@/core/interceptors/api-error.interceptor';

/** The application's HttpClient: fetch backend plus the trace and API-error interceptors. `outer` run before them, so a request they retry is traced again. */
export function provideApiHttpClient(outer: HttpInterceptorFn[] = []) {
  const providers = provideHttpClient(withFetch(), withInterceptors([...outer, TraceInterceptor, apiErrorInterceptor]));

  return providers;
}
