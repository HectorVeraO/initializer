import { HttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import es from '../../../assets/i18n/es.json';
import { provideApiHttpClient } from './api-http.providers';

describe('XSRF protection of same-origin API calls', () => {
  let http: HttpTestingController;

  let client: HttpClient;

  beforeEach(() => {
    document.cookie = 'XSRF-TOKEN=synthetic-xsrf; path=/';
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideApiHttpClient(), provideHttpClientTesting(), provideRouter([]), { provide: MessageService, useValue: { add: vi.fn() } }]
    });
    http = TestBed.inject(HttpTestingController);
    client = TestBed.inject(HttpClient);
  });

  afterEach(() => {
    document.cookie = 'XSRF-TOKEN=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
    http.verify();
  });

  it('sends the cookie value in X-XSRF-TOKEN on a POST', () => {
    client.post('/api/v1/parameters', {}).subscribe();

    const request = http.expectOne('/api/v1/parameters');

    expect(request.request.headers.get('X-XSRF-TOKEN')).toBe('synthetic-xsrf');
    request.flush({});
  });

  it('sends no X-XSRF-TOKEN on a GET', () => {
    client.get('/api/v1/parameters').subscribe();

    const request = http.expectOne('/api/v1/parameters');

    expect(request.request.headers.has('X-XSRF-TOKEN')).toBe(false);
    request.flush({});
  });
});
