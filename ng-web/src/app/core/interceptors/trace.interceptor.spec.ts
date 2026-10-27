import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { environment } from '../../../environments/environment';
import { TraceInterceptor } from './trace.interceptor';

describe('TraceInterceptor', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(withInterceptors([TraceInterceptor])), provideHttpClientTesting()] });
  });

  it('sends a W3C traceparent header on API requests', () => {
    TestBed.inject(HttpClient).get('/api/v1/parameters').subscribe();

    const request = TestBed.inject(HttpTestingController).expectOne('/api/v1/parameters');

    expect(request.request.headers.get('traceparent')).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$/);
  });

  it('sends no traceparent header to a third-party URL', () => {
    TestBed.inject(HttpClient).get('https://elsewhere.test/api/v1/x').subscribe();

    const request = TestBed.inject(HttpTestingController).expectOne('https://elsewhere.test/api/v1/x');

    expect(request.request.headers.has('traceparent')).toBe(false);
  });

  describe('sampled flag', () => {
    const originalRate = environment.trace.samplingRate;

    afterEach(() => {
      environment.trace.samplingRate = originalRate;
      vi.restoreAllMocks();
    });

    function traceparentOfRequest(): string {
      TestBed.inject(HttpClient).get('/api/v1/parameters').subscribe();

      const request = TestBed.inject(HttpTestingController).expectOne('/api/v1/parameters');

      return request.request.headers.get('traceparent') ?? '';
    }

    it('ends with -01 when the draw is below the sampling rate', () => {
      environment.trace.samplingRate = 0.5;
      vi.spyOn(Math, 'random').mockReturnValue(0.1);

      expect(traceparentOfRequest()).toMatch(/-01$/);
    });

    it('ends with -00 when the draw is not below the sampling rate', () => {
      environment.trace.samplingRate = 0.5;
      vi.spyOn(Math, 'random').mockReturnValue(0.9);

      expect(traceparentOfRequest()).toMatch(/-00$/);
    });
  });
});
