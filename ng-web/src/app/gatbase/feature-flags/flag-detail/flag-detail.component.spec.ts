import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { of } from 'rxjs';
import { allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { flagView, tenantOverride, tenantView } from '../../../../testing/fixtures/flag-view';
import { nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { okEnvelope } from '../../../../testing/fixtures/session-view';
import { FlagDetailPage } from './flag-detail.component';

const LIST_URL = '/api/v1/feature-flags';

const TENANTS_URL = '/api/v1/tenants';

const KEY = 'example_outbound_enabled';

const TENANT_A = '44444444-4444-4444-8444-444444444444';

const TENANT_B = '55555555-5555-4555-8555-555555555555';

const UNKNOWN_TENANT = '66666666-6666-4666-8666-666666666666';

const TENANTS = [tenantView(TENANT_A, 'tenant-a', 'Tenant A'), tenantView(TENANT_B, 'tenant-b', 'Tenant B')];

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('FlagDetailPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [],
      [
        { provide: MessageService, useValue: messages },
        { provide: ActivatedRoute, useValue: { paramMap: of(convertToParamMap({ key: KEY })), snapshot: { paramMap: convertToParamMap({ key: KEY }) } } }
      ]
    );
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function answerLoad(flags: Record<string, unknown>[], tenants: Record<string, unknown>[] | 'fail' = TENANTS, totalFlags = flags.length): Promise<void> {
    (await nextCall('GET', LIST_URL)).flush(pageOf(flags, totalFlags));

    const tenantsCall = await nextCall('GET', TENANTS_URL);

    if (tenants === 'fail') {
      tenantsCall.flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'x' });
    } else {
      tenantsCall.flush(pageOf(tenants));
    }
  }

  async function open(flags: Record<string, unknown>[] = [flagView(KEY)], tenants: Record<string, unknown>[] | 'fail' = TENANTS, totalFlags = flags.length): Promise<ComponentFixture<FlagDetailPage>> {
    const fixture = TestBed.createComponent(FlagDetailPage);

    fixture.detectChanges();
    await answerLoad(flags, tenants, totalFlags);
    await settle(fixture);
    // The module translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('próxima actualización');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<FlagDetailPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function rowOf(fixture: ComponentFixture<FlagDetailPage>, key: string): HTMLElement {
    const row = allByTestId(root(fixture), 'flag-tenant-row').find((candidate) => candidate.getAttribute('data-key') === key);

    return row as HTMLElement;
  }

  async function click(fixture: ComponentFixture<FlagDetailPage>, element: HTMLElement | null): Promise<void> {
    (element as HTMLElement).click();
    await settle(fixture);
  }

  async function pickTenant(fixture: ComponentFixture<FlagDetailPage>, slug: string): Promise<void> {
    await click(fixture, byTestId(root(fixture), 'flag-tenant-select'));

    const option = allByTestId(document.body, 'flag-tenant-option').find((candidate) => candidate.getAttribute('data-key') === slug);

    await click(fixture, option as HTMLElement);
  }

  it('reads the flag from the list and the tenants from the tenant list', async () => {
    const fixture = TestBed.createComponent(FlagDetailPage);

    fixture.detectChanges();

    const list = await nextCall('GET', LIST_URL);

    expect(list.request.params.get('size')).toBe('100');
    list.flush(pageOf([flagView(KEY)]));

    const tenants = await nextCall('GET', TENANTS_URL);

    expect(tenants.request.params.get('size')).toBe('100');
    tenants.flush(pageOf([]));
    await settle(fixture);
  });

  it('shows the flag facts, the notice and an empty override list', async () => {
    const fixture = await open([flagView(KEY, { globalOverride: { enabled: false, updatedAt: 'x' } })]);

    expect(byTestId(root(fixture), 'flag-detail-type')?.getAttribute('data-value')).toBe('ops');
    expect(textOf(byTestId(root(fixture), 'flag-detail-global'))).toBe('Inactivo');
    expect(byTestId(root(fixture), 'flag-tenants-empty')).not.toBeNull();
    expect(allByTestId(root(fixture), 'flag-tenant-row')).toHaveLength(0);
  });

  it('shows the expiry of a release flag, also when it expires today', async () => {
    const today = new Date().toISOString().slice(0, 10);

    const fixture = await open([flagView(KEY, { type: 'release', default: false, expiresAt: today })]);

    expect(textOf(byTestId(root(fixture), 'flag-detail-expires'))).toBe(today);
  });

  it('shows a missing key as not found', async () => {
    const fixture = await open([flagView('other_flag')]);

    expect(byTestId(root(fixture), 'flag-detail-not-found')).not.toBeNull();
    expect(byTestId(root(fixture), 'flag-tenant-form')).toBeNull();
  });

  it('warns that a missing key may lie beyond the first page of flags', async () => {
    const fixture = await open([flagView('other_flag')], TENANTS, 150);

    expect(byTestId(root(fixture), 'flag-detail-not-found')).not.toBeNull();
    expect(byTestId(root(fixture), 'flag-detail-truncated')).not.toBeNull();
  });

  it('lists the tenant overrides with name, slug and state, and the id of an unknown tenant', async () => {
    const fixture = await open([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false), tenantOverride(UNKNOWN_TENANT, true)] })]);

    const known = rowOf(fixture, 'tenant-a');

    expect(textOf(byTestId(known, 'flag-tenant-cell-name'))).toBe('Tenant A');
    expect(textOf(byTestId(known, 'flag-tenant-cell-slug'))).toBe('tenant-a');
    expect(byTestId(known, 'flag-tenant-cell-enabled')?.getAttribute('data-value')).toBe('false');

    const unknown = rowOf(fixture, UNKNOWN_TENANT);

    expect(textOf(byTestId(unknown, 'flag-tenant-cell-name'))).toBe(UNKNOWN_TENANT);
    expect(byTestId(unknown, 'flag-tenant-cell-enabled')?.getAttribute('data-value')).toBe('true');
  });

  it('shows tenant ids and a warning when the tenant list fails', async () => {
    const fixture = await open([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, true)] })], 'fail');

    expect(byTestId(root(fixture), 'flag-tenants-failed')).not.toBeNull();
    expect(textOf(byTestId(rowOf(fixture, TENANT_A), 'flag-tenant-cell-name'))).toBe(TENANT_A);
  });

  describe('adding an override', () => {
    it('starts with Save disabled until a tenant is chosen', async () => {
      const fixture = await open();

      expect((byTestId(root(fixture), 'flag-tenant-save') as HTMLButtonElement).disabled).toBe(true);
    });

    it('puts the chosen tenant with enabled true by default and shows the new row', async () => {
      const fixture = await open();

      await pickTenant(fixture, 'tenant-b');
      await click(fixture, byTestId(root(fixture), 'flag-tenant-save'));

      const put = await nextCall('PUT', `${LIST_URL}/${KEY}/tenants/${TENANT_B}`);

      expect(put.request.body).toEqual({ enabled: true });
      put.flush(okEnvelope(flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_B, true)] })));
      await settle(fixture);

      expect(byTestId(rowOf(fixture, 'tenant-b'), 'flag-tenant-cell-enabled')?.getAttribute('data-value')).toBe('true');
      expect((byTestId(root(fixture), 'flag-tenant-save') as HTMLButtonElement).disabled).toBe(true);
    });

    it('sends enabled false when the toggle is switched off', async () => {
      const fixture = await open();

      await pickTenant(fixture, 'tenant-a');
      await click(fixture, byTestId(root(fixture), 'flag-tenant-enabled')?.querySelector('input') ?? null);
      await click(fixture, byTestId(root(fixture), 'flag-tenant-save'));

      const put = await nextCall('PUT', `${LIST_URL}/${KEY}/tenants/${TENANT_A}`);

      expect(put.request.body).toEqual({ enabled: false });
      put.flush(okEnvelope(flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false)] })));
      await settle(fixture);

      expect(byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-cell-enabled')?.getAttribute('data-value')).toBe('false');
    });

    it('toasts and refetches on 404 RESOURCE_NOT_FOUND for a vanished tenant', async () => {
      const fixture = await open();

      await pickTenant(fixture, 'tenant-a');
      await click(fixture, byTestId(root(fixture), 'flag-tenant-save'));
      (await nextCall('PUT', `${LIST_URL}/${KEY}/tenants/${TENANT_A}`)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'x' });
      await answerLoad([flagView(KEY)], [TENANTS[1]]);
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', summary: 'Ese tenant ya no existe. Se actualizó la lista.' }));
      expect(allByTestId(root(fixture), 'flag-tenant-row')).toHaveLength(0);
    });

    it('toasts and shows not found when 404 FLAG_NOT_DEFINED_IN_MANIFEST removes the flag', async () => {
      const fixture = await open();

      await pickTenant(fixture, 'tenant-a');
      await click(fixture, byTestId(root(fixture), 'flag-tenant-save'));
      (await nextCall('PUT', `${LIST_URL}/${KEY}/tenants/${TENANT_A}`)).flush(errorEnvelope('FLAG_NOT_DEFINED_IN_MANIFEST'), { status: 404, statusText: 'x' });
      await answerLoad([flagView('other_flag')]);
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Esa flag ya no está definida en el manifiesto. Se actualizó la lista.' }));
      expect(byTestId(root(fixture), 'flag-detail-not-found')).not.toBeNull();
    });
  });

  describe('removing an override', () => {
    it('deletes after the inline confirmation and drops the row', async () => {
      const fixture = await open([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false)] })]);

      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove'));
      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove-confirm'));

      const del = await nextCall('DELETE', `${LIST_URL}/${KEY}/tenants/${TENANT_A}`);

      del.flush(okEnvelope(flagView(KEY)));
      await settle(fixture);

      expect(allByTestId(root(fixture), 'flag-tenant-row')).toHaveLength(0);
      expect(byTestId(root(fixture), 'flag-tenants-empty')).not.toBeNull();
    });

    it('sends nothing when the confirmation is cancelled', async () => {
      const fixture = await open([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false)] })]);

      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove'));
      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove-cancel'));

      http.expectNone(`${LIST_URL}/${KEY}/tenants/${TENANT_A}`);
      expect(rowOf(fixture, 'tenant-a')).not.toBeUndefined();
    });

    it('toasts and refetches when the removal fails', async () => {
      const fixture = await open([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false)] })]);

      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove'));
      await click(fixture, byTestId(rowOf(fixture, 'tenant-a'), 'flag-tenant-remove-confirm'));
      (await nextCall('DELETE', `${LIST_URL}/${KEY}/tenants/${TENANT_A}`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'x' });
      await answerLoad([flagView(KEY, { tenantOverrides: [tenantOverride(TENANT_A, false)] })]);
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' }));
      expect(rowOf(fixture, 'tenant-a')).not.toBeUndefined();
    });
  });
});
