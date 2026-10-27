import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { RuntimeConfigService } from './runtime-config.service';

const CONFIG_URL = '/assets/config/runtime-config.json';

const VALID_CONFIG = { apiUrl: '' };

describe('RuntimeConfigService', () => {
  let service: RuntimeConfigService;

  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(RuntimeConfigService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
  });

  async function loadWith(body: object): Promise<void> {
    const loading = service.load();

    http.expectOne(CONFIG_URL).flush(body);
    await loading;
  }

  it('exposes a valid file as the config with no failure', async () => {
    await loadWith(VALID_CONFIG);

    expect(service.failure()).toBeNull();
    expect(service.config()).toEqual({ apiUrl: '' });
  });

  it('builds API paths on the same origin when apiUrl is empty', async () => {
    await loadWith(VALID_CONFIG);

    expect(service.apiPath('/api/v1/parameters')).toBe('/api/v1/parameters');
    expect(service.isApiUrl('/api/v1/parameters')).toBe(true);
    expect(service.isApiUrl('https://elsewhere.test/api/v1/x')).toBe(false);
  });

  it('prefixes API paths with a configured apiUrl and trims its trailing slash', async () => {
    await loadWith({ ...VALID_CONFIG, apiUrl: 'https://api.test/' });

    expect(service.apiPath('/api/v1/x')).toBe('https://api.test/api/v1/x');
    expect(service.isApiUrl('https://api.test/api/v1/x')).toBe(true);
  });

  it('reports a missing apiUrl', async () => {
    await loadWith({});

    expect(service.failure()).toEqual({ kind: 'invalid', missingFields: ['apiUrl'] });
  });

  it('rejects an apiUrl of the wrong type', async () => {
    await loadWith({ apiUrl: 42 });

    expect(service.failure()).toEqual({ kind: 'invalid', missingFields: ['apiUrl'] });
  });

  it('reports a load failure when the file answers 404', async () => {
    const loading = service.load();

    http.expectOne(CONFIG_URL).flush('', { status: 404, statusText: 'Not Found' });
    await loading;

    expect(service.failure()).toEqual({ kind: 'load' });
    await expect(service.whenReady()).rejects.toThrow();
  });

  it('requests the file once however many callers wait for it', async () => {
    const first = service.load();

    const second = service.load();

    http.expectOne(CONFIG_URL).flush(VALID_CONFIG);
    await Promise.all([first, second]);

    expect(service.config()).not.toBeNull();
  });
});
