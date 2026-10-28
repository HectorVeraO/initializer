import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import type { Observable } from 'rxjs';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope, okEnvelope } from '../../../testing/fixtures/api-envelope';
import { parameterView } from './testing/parameter-view';
import { ParametersApi } from './parameters-api.service';

describe('ParametersApi', () => {
  let http: HttpTestingController;

  let api: ParametersApi;

  beforeEach(() => {
    configureFeatureTest();
    http = TestBed.inject(HttpTestingController);
    api = TestBed.inject(ParametersApi);
  });

  afterEach(() => {
    http.verify();
  });

  /** Answers the pending call and returns its request. */
  function answered(method: string, url: string, body: object, status = 200) {
    const request = http.expectOne((candidate) => candidate.method === method && candidate.url === url);

    request.flush(body, { status, statusText: 'x' });

    return request.request;
  }

  it('lists the first page at the contract maximum sorted by key', async () => {
    const result = firstValueFrom(api.list());

    const request = answered('GET', '/api/v1/parameters', okEnvelope({ items: [parameterView('example.retry.max_attempts')], page: 1, size: 100, totalItems: 1 }));

    expect(request.params.get('page')).toBe('1');
    expect(request.params.get('size')).toBe('100');
    expect(request.params.get('sort')).toBe('key');
    expect((await result).items.map((item) => item.key)).toEqual(['example.retry.max_attempts']);
  });

  it('creates with the whole definition in the body', async () => {
    const input = { key: 'example.a', valueType: 'number' as const, defaultValue: 1, minValue: 0, maxValue: 5, description: 'x' };

    const result = firstValueFrom(api.create(input));

    const request = answered('POST', '/api/v1/parameters', okEnvelope(parameterView('example.a')), 201);

    expect(request.body).toEqual(input);
    expect((await result).key).toBe('example.a');
  });

  it('replaces by key without a key in the body', async () => {
    const result = firstValueFrom(api.update('example.a', { valueType: 'number', defaultValue: 2 }));

    const request = answered('PUT', '/api/v1/parameters/example.a', okEnvelope(parameterView('example.a', { defaultValue: 2 })));

    expect(request.body).toEqual({ valueType: 'number', defaultValue: 2 });
    expect((await result).defaultValue).toBe(2);
  });

  it.each<[string, string, string, (api: ParametersApi, key: string) => Observable<{ key: string }>]>([
    ['update', 'PUT', '', (api: ParametersApi, key: string) => api.update(key, { valueType: 'number', defaultValue: 1 })],
    ['remove', 'DELETE', '', (api: ParametersApi, key: string) => api.remove(key)],
    ['setOverride', 'PUT', '/tenant-override', (api: ParametersApi, key: string) => api.setOverride(key, 5)],
    ['clearOverride', 'DELETE', '/tenant-override', (api: ParametersApi, key: string) => api.clearOverride(key)]
  ])('escapes the key of %s as one path segment', async (_name, method, suffix, call) => {
    const key = 'weird key/with?chars#and%';

    const result = firstValueFrom(call(api, key));

    answered(method, `/api/v1/parameters/${encodeURIComponent(key)}${suffix}`, okEnvelope(parameterView(key)));

    expect((await result).key).toBe(key);
  });

  it('sends the override value in the body', async () => {
    const result = firstValueFrom(api.setOverride('example.a', { nested: [1] }));

    const request = answered('PUT', '/api/v1/parameters/example.a/tenant-override', okEnvelope(parameterView('example.a', { effectiveValue: 5, source: 'tenant' })));

    expect(request.body).toEqual({ value: { nested: [1] } });
    expect((await result).source).toBe('tenant');
  });

  it('removes a definition and reports how many overrides went with it', async () => {
    const result = firstValueFrom(api.remove('example.a'));

    answered('DELETE', '/api/v1/parameters/example.a', okEnvelope({ key: 'example.a', deletedOverrideCount: 2 }));

    expect(await result).toEqual({ key: 'example.a', deletedOverrideCount: 2 });
  });

  it('clears an override', async () => {
    const result = firstValueFrom(api.clearOverride('example.a'));

    answered('DELETE', '/api/v1/parameters/example.a/tenant-override', okEnvelope(parameterView('example.a')));

    expect((await result).source).toBe('global');
  });

  it('rejects with the HTTP error so the view can map the code', async () => {
    const result = firstValueFrom(api.remove('example.a')).catch((error: { status: number }) => error.status);

    answered('DELETE', '/api/v1/parameters/example.a', errorEnvelope('SYSTEM_PARAMETER_PROTECTED'), 409);

    expect(await result).toBe(409);
  });
});
