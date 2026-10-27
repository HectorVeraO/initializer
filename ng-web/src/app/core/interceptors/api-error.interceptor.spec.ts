import { HttpClient, HttpContext, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import es from '../../../assets/i18n/es.json';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { apiErrorInterceptor } from './api-error.interceptor';
import { BlankComponent } from '@/core/testing/ui-testing';

function apiError(code: string): object {
  return { data: null, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: { code, message: 'text', issues: [] } };
}

describe('apiErrorInterceptor', () => {
  let http: HttpTestingController;

  let client: HttpClient;

  let router: Router;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(async () => {
    messages = { add: vi.fn() };
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideHttpClient(withInterceptors([apiErrorInterceptor])), provideHttpClientTesting(), provideRouter([{ path: '**', component: BlankComponent }]), { provide: MessageService, useValue: messages }]
    });
    http = TestBed.inject(HttpTestingController);
    client = TestBed.inject(HttpClient);
    router = TestBed.inject(Router);
    await router.navigateByUrl('/home');
  });

  afterEach(() => {
    http.verify();
  });

  function fail(url: string, status: number, body: object | null = null): void {
    client.get(url).subscribe({ error: () => undefined });
    http.expectOne(url).flush(body, { status, statusText: 'x' });
  }

  // Router navigation completes asynchronously; whenStable waits for any navigation the interceptor started.
  async function settle(): Promise<void> {
    await TestBed.inject(ApplicationRef).whenStable();
  }

  it('sends the person to /forbidden on a GET answering 403 INSUFFICIENT_ROLE', async () => {
    fail('/api/v1/parameters', 403, apiError('INSUFFICIENT_ROLE'));
    await settle();

    expect(router.url).toBe('/forbidden');
  });

  it('sends the person to /forbidden on a HEAD answering 403 INSUFFICIENT_ROLE', async () => {
    client.head('/api/v1/parameters').subscribe({ error: () => undefined });
    http.expectOne('/api/v1/parameters').flush(apiError('INSUFFICIENT_ROLE'), { status: 403, statusText: 'x' });
    await settle();

    expect(router.url).toBe('/forbidden');
  });

  it('sends the person to /forbidden on a GET answering 403 PLATFORM_OPERATOR_REQUIRED', async () => {
    fail('/api/v1/tenants', 403, apiError('PLATFORM_OPERATOR_REQUIRED'));
    await settle();

    expect(router.url).toBe('/forbidden');
  });

  it('navigates nowhere when a POST answers 403 INSUFFICIENT_ROLE', async () => {
    client.post('/api/v1/parameters', {}).subscribe({ error: () => undefined });
    http.expectOne('/api/v1/parameters').flush(apiError('INSUFFICIENT_ROLE'), { status: 403, statusText: 'x' });
    await settle();

    expect(router.url).toBe('/home');
  });

  it('does not treat a 403 with another code as a permission problem', async () => {
    fail('/api/v1/parameters', 403, apiError('TENANT_SUSPENDED'));
    await settle();

    expect(router.url).toBe('/home');
  });

  it('leaves a 403 from a URL that is not the API alone', async () => {
    fail('https://elsewhere.test/other', 403, apiError('INSUFFICIENT_ROLE'));
    await settle();

    expect(router.url).toBe('/home');
  });

  it('shows a generic toast on a server error and leaves the screen where it is', async () => {
    fail('/api/v1/parameters', 503, apiError('SERVICE_UNAVAILABLE'));
    await settle();

    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: `${es.errors.operationFailed} Referencia: abc` }));
    expect(router.url).toBe('/home');
  });

  it('shows the trace id from the X-Trace-Id header when the failure has no envelope', () => {
    client.get('/api/v1/parameters').subscribe({ error: () => undefined });
    http.expectOne('/api/v1/parameters').flush(null, { status: 502, statusText: 'Bad Gateway', headers: { 'X-Trace-Id': '4bf92f3577b34da6a3ce929d0e0e4736' } });

    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ detail: `${es.errors.operationFailed} Referencia: 4bf92f3577b34da6a3ce929d0e0e4736` }));
  });

  it('shows the generic toast without a reference when the failure carries no trace id', () => {
    fail('/api/v1/parameters', 500, { data: null, meta: {}, error: { code: 'INTERNAL_ERROR', message: 'text', issues: [] } });

    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ detail: es.errors.operationFailed }));
  });

  it('shows the generic toast on a network error', () => {
    fail('/api/v1/parameters', 0);

    expect(messages.add).toHaveBeenCalledTimes(1);
  });

  it('raises no toast for a caller that shows its own error state', () => {
    client.get('/api/v1/parameters', { context: new HttpContext().set(SKIP_ERROR_TOAST, true) }).subscribe({ error: () => undefined });
    http.expectOne('/api/v1/parameters').flush(null, { status: 503, statusText: 'Unavailable' });

    expect(messages.add).not.toHaveBeenCalled();
  });
});
