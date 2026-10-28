import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, stubScrollIntoView, textOf } from '@/core/testing/ui-testing';
import { errorEnvelope, okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { flagView, tenantOverride } from '../../../../testing/fixtures/flag-view';
import { FlagListPage } from './flag-list.component';

const LIST_URL = '/api/v1/feature-flags';

const OPS = 'example_outbound_enabled';

const RELEASE = 'new_settings_ui';

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('FlagListPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    stubScrollIntoView();
    messages = { add: vi.fn() };
    configureFeatureTest([], [{ provide: MessageService, useValue: messages }]);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function open(items: Record<string, unknown>[] = [flagView(OPS), flagView(RELEASE, { type: 'release', default: false, expiresAt: '2099-12-31' })], totalItems = items.length): Promise<ComponentFixture<FlagListPage>> {
    const fixture = TestBed.createComponent(FlagListPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, totalItems));
    await settle(fixture);
    // The module translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Override global');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<FlagListPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function rowOf(fixture: ComponentFixture<FlagListPage>, key: string): HTMLElement {
    const row = allByTestId(root(fixture), 'flags-row').find((candidate) => candidate.getAttribute('data-key') === key);

    return row as HTMLElement;
  }

  async function pick(fixture: ComponentFixture<FlagListPage>, key: string, choice: 'inherit' | 'on' | 'off'): Promise<void> {
    (byTestId(rowOf(fixture, key), 'flag-global-select') as HTMLElement).click();
    await settle(fixture);

    const option = allByTestId(document.body, 'flag-global-option').find((candidate) => candidate.getAttribute('data-key') === choice);

    (option as HTMLElement).click();
    await settle(fixture);
  }

  it('requests the first page of 100 without a sort', async () => {
    const fixture = TestBed.createComponent(FlagListPage);

    fixture.detectChanges();

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('page')).toBe('1');
    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.has('sort')).toBe(false);
    request.flush(pageOf([]));
    await settle(fixture);
  });

  it('shows the type, default and tenant override count of each flag, and the expiry of a release flag only', async () => {
    const fixture = await open([flagView(OPS, { tenantOverrides: [tenantOverride('t1', true), tenantOverride('t2', false)] }), flagView(RELEASE, { type: 'release', default: false, expiresAt: '2099-12-31' })]);

    expect(byTestId(rowOf(fixture, OPS), 'flags-cell-type')?.getAttribute('data-value')).toBe('ops');
    expect(textOf(byTestId(rowOf(fixture, OPS), 'flags-cell-default'))).toBe('Activo');
    expect(textOf(byTestId(rowOf(fixture, OPS), 'flags-cell-tenants'))).toBe('2');
    expect(textOf(byTestId(rowOf(fixture, OPS), 'flags-cell-expires'))).toBe('-');
    expect(byTestId(rowOf(fixture, RELEASE), 'flags-cell-type')?.getAttribute('data-value')).toBe('release');
    expect(textOf(byTestId(rowOf(fixture, RELEASE), 'flags-cell-default'))).toBe('Inactivo');
    expect(textOf(byTestId(rowOf(fixture, RELEASE), 'flags-cell-expires'))).toBe('2099-12-31');
  });

  it('shows a release flag that expires today', async () => {
    const today = new Date().toISOString().slice(0, 10);

    const fixture = await open([flagView(RELEASE, { type: 'release', default: false, expiresAt: today })]);

    expect(textOf(byTestId(rowOf(fixture, RELEASE), 'flags-cell-expires'))).toBe(today);
  });

  it('links each row to its detail and shows the notice', async () => {
    const fixture = await open();

    expect(byTestId(rowOf(fixture, OPS), 'flag-open')?.getAttribute('href')).toBe(`/operator/feature-flags/${OPS}`);
    expect(textOf(byTestId(root(fixture), 'flags-notice'))).toContain('próxima actualización de su sesión');
  });

  it('says so when there are no flags', async () => {
    const fixture = TestBed.createComponent(FlagListPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    expect(byTestId(root(fixture), 'flags-empty')).not.toBeNull();
    expect(allByTestId(root(fixture), 'flags-row')).toHaveLength(0);
  });

  it('shows the load error with a retry that asks again', async () => {
    const fixture = TestBed.createComponent(FlagListPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'x' });
    await settle(fixture);
    expect(byTestId(root(fixture), 'flags-load-error')).not.toBeNull();
    (byTestId(root(fixture), 'flags-retry') as HTMLElement).click();
    (await nextCall('GET', LIST_URL)).flush(pageOf([flagView(OPS)]));
    await settle(fixture);
    expect(allByTestId(root(fixture), 'flags-row')).toHaveLength(1);
  });

  it('says when the list holds only part of the flags', async () => {
    const fixture = await open([flagView(OPS)], 250);

    expect(textOf(byTestId(root(fixture), 'flags-truncated'))).toBe('Mostrando 1 de 250 flags.');
  });

  describe('global override', () => {
    it.each([
      ['no override', null, 'inherit'],
      ['an enabled override', { enabled: true, updatedAt: 'x' }, 'on'],
      ['a disabled override', { enabled: false, updatedAt: 'x' }, 'off']
    ])('reads the current value from %s', async (_name, globalOverride, expected) => {
      const fixture = await open([flagView(OPS, { globalOverride })]);

      expect(byTestId(rowOf(fixture, OPS), 'flags-cell-global')?.getAttribute('data-value')).toBe(expected);
    });

    it('sends PUT { enabled: true } for On and shows the stored override', async () => {
      const fixture = await open();

      await pick(fixture, OPS, 'on');

      const put = await nextCall('PUT', `${LIST_URL}/${OPS}`);

      expect(put.request.body).toEqual({ enabled: true });
      put.flush(okEnvelope(flagView(OPS, { globalOverride: { enabled: true, updatedAt: 'x' } })));
      await settle(fixture);

      expect(byTestId(rowOf(fixture, OPS), 'flags-cell-global')?.getAttribute('data-value')).toBe('on');
    });

    it('sends PUT { enabled: false } for Off', async () => {
      const fixture = await open();

      await pick(fixture, OPS, 'off');

      const put = await nextCall('PUT', `${LIST_URL}/${OPS}`);

      expect(put.request.body).toEqual({ enabled: false });
      put.flush(okEnvelope(flagView(OPS, { globalOverride: { enabled: false, updatedAt: 'x' } })));
      await settle(fixture);

      expect(byTestId(rowOf(fixture, OPS), 'flags-cell-global')?.getAttribute('data-value')).toBe('off');
    });

    it('sends DELETE for Inherit', async () => {
      const fixture = await open([flagView(OPS, { globalOverride: { enabled: false, updatedAt: 'x' } })]);

      await pick(fixture, OPS, 'inherit');

      const del = await nextCall('DELETE', `${LIST_URL}/${OPS}`);

      expect(del.request.body).toBeNull();
      del.flush(okEnvelope(flagView(OPS)));
      await settle(fixture);

      expect(byTestId(rowOf(fixture, OPS), 'flags-cell-global')?.getAttribute('data-value')).toBe('inherit');
    });

    it('sends nothing when the current choice is picked again', async () => {
      const fixture = await open([flagView(OPS, { globalOverride: { enabled: true, updatedAt: 'x' } })]);

      await pick(fixture, OPS, 'on');

      http.expectNone(`${LIST_URL}/${OPS}`);
    });

    it('toasts and refetches on 404 FLAG_NOT_DEFINED_IN_MANIFEST, dropping the removed flag', async () => {
      const fixture = await open();

      await pick(fixture, OPS, 'off');
      (await nextCall('PUT', `${LIST_URL}/${OPS}`)).flush(errorEnvelope('FLAG_NOT_DEFINED_IN_MANIFEST'), { status: 404, statusText: 'x' });
      (await nextCall('GET', LIST_URL)).flush(pageOf([flagView(RELEASE, { type: 'release', default: false, expiresAt: '2099-12-31' })]));
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', summary: 'Esa flag ya no está definida en el manifiesto. Se actualizó la lista.' }));
      expect(allByTestId(root(fixture), 'flags-row')).toHaveLength(1);
      expect(rowOf(fixture, OPS)).toBeUndefined();
    });

    it('toasts and shows the server value again after another failure', async () => {
      const fixture = await open();

      await pick(fixture, OPS, 'off');
      (await nextCall('PUT', `${LIST_URL}/${OPS}`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'x' });
      (await nextCall('GET', LIST_URL)).flush(pageOf([flagView(OPS)]));
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' }));
      expect(byTestId(rowOf(fixture, OPS), 'flags-cell-global')?.getAttribute('data-value')).toBe('inherit');
    });
  });
});
