import { provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import { TraceInterceptor } from '@/core/interceptors/trace.interceptor';
import { apiErrorInterceptor } from '@/core/interceptors/api-error.interceptor';

/** The application's HttpClient: fetch backend plus the trace and API-error interceptors. */
export function provideApiHttpClient() {
  const providers = provideHttpClient(withFetch(), withInterceptors([TraceInterceptor, apiErrorInterceptor]));

  return providers;
}
