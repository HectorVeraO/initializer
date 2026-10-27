import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, UrlTree, provideRouter } from '@angular/router';
import { configGuard } from './config.guard';

const CONFIG_URL = '/assets/config/runtime-config.json';

describe('configGuard', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()] });
  });

  function run(): Promise<boolean | UrlTree> {
    return TestBed.runInInjectionContext(() => configGuard({} as never, {} as never)) as Promise<boolean | UrlTree>;
  }

  it('lets the navigation through when the config loaded', async () => {
    const outcome = run();

    TestBed.inject(HttpTestingController).expectOne(CONFIG_URL).flush({ apiUrl: '' });

    expect(await outcome).toBe(true);
  });

  it('redirects to the error page when the config is unusable', async () => {
    const outcome = run();

    TestBed.inject(HttpTestingController).expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' });

    const result = await outcome;

    expect(TestBed.inject(Router).serializeUrl(result as UrlTree)).toBe('/error-page');
  });
});
