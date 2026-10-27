import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import type { Observable } from 'rxjs';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../testing/fixtures/api-envelope';
import { flagView } from '../../../testing/fixtures/flag-view';
import { okEnvelope } from '../../../testing/fixtures/session-view';
import { FeatureFlagsApi } from './feature-flags-api.service';

const TENANT_ID = '44444444-4444-4444-8444-444444444444';

describe('FeatureFlagsApi', () => {
  let http: HttpTestingController;

  let api: FeatureFlagsApi;

  beforeEach(() => {
    configureFeatureTest();
    http = TestBed.inject(HttpTestingController);
    api = TestBed.inject(FeatureFlagsApi);
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

  it('lists the first page at the contract maximum without a sort', async () => {
    const result = firstValueFrom(api.list());

    const request = answered('GET', '/api/v1/feature-flags', okEnvelope({ items: [flagView('example_outbound_enabled')], page: 1, size: 100, totalItems: 1 }));

    expect(request.params.get('page')).toBe('1');
    expect(request.params.get('size')).toBe('100');
    expect(request.params.has('sort')).toBe(false);
    expect((await result).items.map((item) => item.key)).toEqual(['example_outbound_enabled']);
  });

  it('sets the global override with the enabled flag in the body', async () => {
    const result = firstValueFrom(api.setGlobal('new_settings_ui', false));

    const request = answered('PUT', '/api/v1/feature-flags/new_settings_ui', okEnvelope(flagView('new_settings_ui', { globalOverride: { enabled: false, updatedAt: 'x' } })));

    expect(request.body).toEqual({ enabled: false });
    expect((await result).globalOverride?.enabled).toBe(false);
  });

  it('clears the global override with a DELETE and no body', async () => {
    const result = firstValueFrom(api.clearGlobal('new_settings_ui'));

    const request = answered('DELETE', '/api/v1/feature-flags/new_settings_ui', okEnvelope(flagView('new_settings_ui')));

    expect(request.body).toBeNull();
    await result;
  });

  it('sets a tenant override on the tenant path', async () => {
    const result = firstValueFrom(api.setTenant('new_settings_ui', TENANT_ID, true));

    const request = answered('PUT', `/api/v1/feature-flags/new_settings_ui/tenants/${TENANT_ID}`, okEnvelope(flagView('new_settings_ui')));

    expect(request.body).toEqual({ enabled: true });
    expect((await result).key).toBe('new_settings_ui');
  });

  it('clears a tenant override on the tenant path', async () => {
    const result = firstValueFrom(api.clearTenant('new_settings_ui', TENANT_ID));

    answered('DELETE', `/api/v1/feature-flags/new_settings_ui/tenants/${TENANT_ID}`, okEnvelope(flagView('new_settings_ui')));

    expect((await result).key).toBe('new_settings_ui');
  });

  it.each<[string, string, string, (api: FeatureFlagsApi, key: string) => Observable<{ key: string }>]>([
    ['setGlobal', 'PUT', '', (api: FeatureFlagsApi, key: string) => api.setGlobal(key, true)],
    ['clearGlobal', 'DELETE', '', (api: FeatureFlagsApi, key: string) => api.clearGlobal(key)],
    ['setTenant', 'PUT', '/tenants/t%2F1%3F', (api: FeatureFlagsApi, key: string) => api.setTenant(key, 't/1?', true)],
    ['clearTenant', 'DELETE', '/tenants/t%2F1%3F', (api: FeatureFlagsApi, key: string) => api.clearTenant(key, 't/1?')]
  ])('escapes the key and tenant id of %s as single path segments', async (_name, method, suffix, call) => {
    const key = 'weird key/with?chars#and%';

    const result = firstValueFrom(call(api, key));

    answered(method, `/api/v1/feature-flags/${encodeURIComponent(key)}${suffix}`, okEnvelope(flagView(key)));

    expect((await result).key).toBe(key);
  });

  it('rejects with the HTTP error so the view can map the code', async () => {
    const result = firstValueFrom(api.clearGlobal('gone')).catch((error: { status: number }) => error.status);

    answered('DELETE', '/api/v1/feature-flags/gone', errorEnvelope('FLAG_NOT_DEFINED_IN_MANIFEST'), 404);

    expect(await result).toBe(404);
  });
});
