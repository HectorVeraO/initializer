import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { SessionStore } from '@/gatbase/core';
import { allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { parameterView } from '../../../../testing/fixtures/parameter-view';
import { loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { ParameterListPage } from './parameter-list.component';

const LIST_URL = '/api/v1/parameters';

const KEY = 'example.retry.max_attempts';

const OTHER_TENANT = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

function role(name: 'admin' | 'member' | 'viewer', isPlatformOperator = false): Record<string, unknown> {
  return sessionView({ account: { id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', fullName: 'Jane Doe', role: name, isPlatformOperator } });
}

const OPERATOR_WITHOUT_TENANT = sessionView({
  account: { id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', fullName: 'Jane Doe', role: null, isPlatformOperator: true },
  activeTenant: null,
  memberships: []
});

describe('ParameterListPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest([], [{ provide: MessageService, useValue: messages }]);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function open(session: Record<string, unknown>, items: Record<string, unknown>[] = [parameterView(KEY)], totalItems = items.length): Promise<ComponentFixture<ParameterListPage>> {
    await loadSession(session);

    const fixture = TestBed.createComponent(ParameterListPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, totalItems));
    await settle(fixture);
    // The module translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Parámetros');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<ParameterListPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function rowOf(fixture: ComponentFixture<ParameterListPage>, key: string): HTMLElement {
    const row = allByTestId(root(fixture), 'parameters-row').find((candidate) => candidate.getAttribute('data-key') === key);

    return row as HTMLElement;
  }

  async function click(fixture: ComponentFixture<ParameterListPage>, element: HTMLElement | null): Promise<void> {
    (element as HTMLElement).click();
    await settle(fixture);
  }

  async function confirm(fixture: ComponentFixture<ParameterListPage>, key: string, testId: string): Promise<void> {
    await click(fixture, byTestId(rowOf(fixture, key), testId));
    await click(fixture, byTestId(rowOf(fixture, key), `${testId}-confirm`));
  }

  it('requests the first page of 100 sorted by key', async () => {
    await loadSession(role('admin'));

    const fixture = TestBed.createComponent(ParameterListPage);

    fixture.detectChanges();

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('page')).toBe('1');
    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.get('sort')).toBe('key');
    request.flush(pageOf([]));
    await settle(fixture);
  });

  describe('listing', () => {
    async function openMixed(): Promise<ComponentFixture<ParameterListPage>> {
      const fixture = await open(role('admin'), [
        parameterView(KEY, { effectiveValue: 5, source: 'tenant' }),
        parameterView('example.mode', { valueType: 'string', effectiveValue: 'fast', defaultValue: 'slow', minValue: null, maxValue: null, allowedValues: ['fast', 'slow'], isSystemRequired: true }),
        parameterView('example.settings', { valueType: 'json', effectiveValue: { a: [1] }, defaultValue: {}, minValue: null, maxValue: null })
      ]);

      return fixture;
    }

    it('shows value, source, default and bounds of a numeric parameter', async () => {
      const row = rowOf(await openMixed(), KEY);

      expect(textOf(byTestId(row, 'parameters-cell-value'))).toBe('5');
      expect(textOf(byTestId(row, 'parameters-cell-source'))).toBe('Tenant');
      expect(byTestId(row, 'parameters-cell-source')?.getAttribute('data-value')).toBe('tenant');
      expect(textOf(byTestId(row, 'parameters-cell-default'))).toBe('3');
      expect(textOf(byTestId(row, 'parameters-cell-bounds'))).toBe('1..10');
    });

    it('shows the allowed values and the system badge of a system parameter', async () => {
      const row = rowOf(await openMixed(), 'example.mode');

      expect(textOf(byTestId(row, 'parameters-cell-value'))).toBe('fast');
      expect(byTestId(row, 'parameters-cell-source')?.getAttribute('data-value')).toBe('global');
      expect(textOf(byTestId(row, 'parameters-cell-bounds'))).toBe('Uno de: fast, slow');
      expect(textOf(byTestId(row, 'parameters-cell-system'))).toBe('Sistema');
    });

    it('shows a json value as compact json and no system badge on a regular parameter', async () => {
      const fixture = await openMixed();

      expect(textOf(byTestId(rowOf(fixture, 'example.settings'), 'parameters-cell-value'))).toBe('{"a":[1]}');
      expect(textOf(byTestId(rowOf(fixture, KEY), 'parameters-cell-system'))).toBe('');
    });

    it('does not warn about truncation when every parameter is listed', async () => {
      const fixture = await openMixed();

      expect(byTestId(root(fixture), 'parameters-truncated')).toBeNull();
    });
  });

  it('says when the list holds only part of the parameters', async () => {
    const fixture = await open(role('admin'), [parameterView(KEY)], 250);

    expect(textOf(byTestId(root(fixture), 'parameters-truncated'))).toBe('Mostrando 1 de 250 parámetros.');
  });

  it('gives an admin the override controls and no operator controls', async () => {
    const fixture = await open(role('admin'), [parameterView(KEY), parameterView('example.b', { source: 'tenant' })]);

    expect(byTestId(rowOf(fixture, KEY), 'parameter-set-override')).not.toBeNull();
    expect(byTestId(rowOf(fixture, KEY), 'parameter-clear-override')).toBeNull();
    expect(byTestId(rowOf(fixture, 'example.b'), 'parameter-clear-override')).not.toBeNull();
    expect(byTestId(root(fixture), 'parameter-new')).toBeNull();
    expect(byTestId(root(fixture), 'parameter-edit')).toBeNull();
    expect(byTestId(root(fixture), 'parameter-delete')).toBeNull();
  });

  it.each([['member' as const], ['viewer' as const]])('shows a %s the list without any control', async (name) => {
    const fixture = await open(role(name));

    expect(allByTestId(root(fixture), 'parameters-row')).toHaveLength(1);
    for (const testId of ['parameter-set-override', 'parameter-clear-override', 'parameter-new', 'parameter-edit', 'parameter-delete']) {
      expect(byTestId(root(fixture), testId)).toBeNull();
    }
  });

  it('gives an operator without a tenant the definition controls and no override controls', async () => {
    const fixture = await open(OPERATOR_WITHOUT_TENANT);

    expect(byTestId(root(fixture), 'parameter-new')).not.toBeNull();
    expect(byTestId(rowOf(fixture, KEY), 'parameter-edit')).not.toBeNull();
    expect(byTestId(rowOf(fixture, KEY), 'parameter-delete')).not.toBeNull();
    expect(byTestId(root(fixture), 'parameter-set-override')).toBeNull();
    expect(byTestId(root(fixture), 'parameter-clear-override')).toBeNull();
    expect(byTestId(rowOf(fixture, KEY), 'parameters-cell-source')?.getAttribute('data-value')).toBe('global');
  });

  it('gives an operator who is also admin of the active tenant both sets of controls', async () => {
    const fixture = await open(role('admin', true));

    expect(byTestId(rowOf(fixture, KEY), 'parameter-set-override')).not.toBeNull();
    expect(byTestId(rowOf(fixture, KEY), 'parameter-edit')).not.toBeNull();
  });

  it('disables Delete for a system parameter and sends nothing', async () => {
    const fixture = await open(OPERATOR_WITHOUT_TENANT, [parameterView(KEY, { isSystemRequired: true }), parameterView('example.b')]);

    const button = byTestId(rowOf(fixture, KEY), 'parameter-delete') as HTMLButtonElement;

    expect(button.disabled).toBe(true);
    expect((byTestId(rowOf(fixture, 'example.b'), 'parameter-delete') as HTMLButtonElement).disabled).toBe(false);
    await click(fixture, button);
    expect(byTestId(rowOf(fixture, KEY), 'parameter-delete-confirm')).toBeNull();
  });

  describe('overrides', () => {
    async function setOverride(fixture: ComponentFixture<ParameterListPage>, typed: string): Promise<void> {
      await click(fixture, byTestId(rowOf(fixture, KEY), 'parameter-set-override'));
      await vi.waitFor(() => expect(byTestId(document.body, 'parameter-override-value')).not.toBeNull());

      const input = (byTestId(document.body, 'parameter-override-value') as HTMLElement).querySelector('input') as HTMLInputElement;

      typeInto(input, typed);
      input.dispatchEvent(new Event('blur'));
      await click(fixture, byTestId(document.body, 'parameter-override-save'));
    }

    it('sets an override, then shows the new value and source from the refetch', async () => {
      const fixture = await open(role('admin'));

      await setOverride(fixture, '5');

      const put = await nextCall('PUT', `${LIST_URL}/${KEY}/tenant-override`);

      expect(put.request.body).toEqual({ value: 5 });
      put.flush(okEnvelope(parameterView(KEY, { effectiveValue: 5, source: 'tenant' })));
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY, { effectiveValue: 5, source: 'tenant' })]));
      await settle(fixture);

      expect(textOf(byTestId(rowOf(fixture, KEY), 'parameters-cell-value'))).toBe('5');
      expect(byTestId(rowOf(fixture, KEY), 'parameters-cell-source')?.getAttribute('data-value')).toBe('tenant');
      expect(byTestId(document.body, 'parameter-override-value')).toBeNull();
    });

    it('keeps the dialog open with the refusal under the input when the server says 422', async () => {
      const fixture = await open(role('admin'));

      await setOverride(fixture, '7');
      (await nextCall('PUT', `${LIST_URL}/${KEY}/tenant-override`)).flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'value', code: 'PARAMETER_OUT_OF_RANGE', message: 'x' }]), { status: 422, statusText: 'x' });
      await settle(fixture);

      expect(textOf(byTestId(document.body, 'parameter-override-value-error'))).toBe('El valor está fuera del rango permitido.');
    });

    it('clears an override after the inline confirmation and shows the global value again', async () => {
      const fixture = await open(role('admin'), [parameterView(KEY, { effectiveValue: 5, source: 'tenant' })]);

      await confirm(fixture, KEY, 'parameter-clear-override');
      (await nextCall('DELETE', `${LIST_URL}/${KEY}/tenant-override`)).flush(okEnvelope(parameterView(KEY)));
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY)]));
      await settle(fixture);

      expect(textOf(byTestId(rowOf(fixture, KEY), 'parameters-cell-value'))).toBe('3');
      expect(byTestId(rowOf(fixture, KEY), 'parameters-cell-source')?.getAttribute('data-value')).toBe('global');
      expect(byTestId(rowOf(fixture, KEY), 'parameter-clear-override')).toBeNull();
    });

    it('sends nothing when the clear confirmation is cancelled', async () => {
      const fixture = await open(role('admin'), [parameterView(KEY, { source: 'tenant' })]);

      await click(fixture, byTestId(rowOf(fixture, KEY), 'parameter-clear-override'));
      await click(fixture, byTestId(rowOf(fixture, KEY), 'parameter-clear-override-cancel'));

      expect(byTestId(rowOf(fixture, KEY), 'parameter-clear-override')).not.toBeNull();
    });
  });

  describe('definitions', () => {
    it('deletes after the inline confirmation and names how many overrides went with it', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT, [parameterView(KEY), parameterView('example.b')]);

      await confirm(fixture, KEY, 'parameter-delete');
      (await nextCall('DELETE', `${LIST_URL}/${KEY}`)).flush(okEnvelope({ key: KEY, deletedOverrideCount: 3 }));
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView('example.b')]));
      await settle(fixture);

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', summary: `Parámetro ${KEY} eliminado junto con 3 valores propios de tenants.` }));
      expect(allByTestId(root(fixture), 'parameters-row').map((row) => row.getAttribute('data-key'))).toEqual(['example.b']);
    });

    it('puts a SYSTEM_PARAMETER_PROTECTED refusal on the row', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT, [parameterView(KEY), parameterView('example.b')]);

      await confirm(fixture, KEY, 'parameter-delete');
      (await nextCall('DELETE', `${LIST_URL}/${KEY}`)).flush(errorEnvelope('SYSTEM_PARAMETER_PROTECTED'), { status: 409, statusText: 'x' });
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY), parameterView('example.b')]));
      await settle(fixture);

      await vi.waitFor(() => {
        fixture.detectChanges();
        expect(textOf(byTestId(rowOf(fixture, KEY), 'parameter-row-error'))).toBe('Este parámetro lo requiere el sistema y no se puede eliminar.');
      });
      expect(byTestId(rowOf(fixture, 'example.b'), 'parameter-row-error')).toBeNull();
    });

    it('tells that the row is gone after a 404 on delete', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT);

      await confirm(fixture, KEY, 'parameter-delete');
      (await nextCall('DELETE', `${LIST_URL}/${KEY}`)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'x' });
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY)]));
      await settle(fixture);

      await vi.waitFor(() => {
        fixture.detectChanges();
        expect(textOf(byTestId(rowOf(fixture, KEY), 'parameter-row-error'))).toContain('ya no existe');
      });
    });

    it('creates a parameter from the dialog and refetches the list', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT, []);

      await click(fixture, byTestId(root(fixture), 'parameter-new'));
      await vi.waitFor(() => expect(byTestId(document.body, 'parameter-def-key')).not.toBeNull());
      typeInto(byTestId(document.body, 'parameter-def-key') as HTMLInputElement, 'example.mode');
      typeInto(byTestId(document.body, 'parameter-def-default') as HTMLInputElement, 'fast');
      await click(fixture, byTestId(document.body, 'parameter-def-save'));

      const post = await nextCall('POST', LIST_URL);

      expect(post.request.body).toMatchObject({ key: 'example.mode', valueType: 'string', defaultValue: 'fast' });
      post.flush(okEnvelope(parameterView('example.mode', { valueType: 'string', effectiveValue: 'fast', defaultValue: 'fast' })), { status: 201, statusText: 'x' });
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView('example.mode', { valueType: 'string', effectiveValue: 'fast', defaultValue: 'fast' })]));
      await settle(fixture);

      expect(rowOf(fixture, 'example.mode')).toBeDefined();
    });

    it('opens the edit dialog on the row definition', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT);

      await click(fixture, byTestId(rowOf(fixture, KEY), 'parameter-edit'));
      await vi.waitFor(() => expect((byTestId(document.body, 'parameter-def-key') as HTMLInputElement).value).toBe(KEY));
    });

    it('saves an edit and shows the refetched definition', async () => {
      const fixture = await open(OPERATOR_WITHOUT_TENANT);

      await click(fixture, byTestId(rowOf(fixture, KEY), 'parameter-edit'));
      await vi.waitFor(() => expect((byTestId(document.body, 'parameter-def-key') as HTMLInputElement).value).toBe(KEY));
      typeInto(byTestId(document.body, 'parameter-def-default') as HTMLInputElement, '4');
      await click(fixture, byTestId(document.body, 'parameter-def-save'));

      const put = await nextCall('PUT', `${LIST_URL}/${KEY}`);

      expect(put.request.body).toMatchObject({ defaultValue: 4 });
      put.flush(okEnvelope(parameterView(KEY, { defaultValue: 4, effectiveValue: 4 })));
      (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY, { defaultValue: 4, effectiveValue: 4 })]));
      await settle(fixture);

      expect(textOf(byTestId(rowOf(fixture, KEY), 'parameters-cell-default'))).toBe('4');
    });
  });

  it('refetches when the active tenant changes', async () => {
    const fixture = await open(role('admin'));

    const switching = TestBed.inject(SessionStore).switchTenant(OTHER_TENANT.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: OTHER_TENANT })));
    await switching;
    (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView('example.other', { effectiveValue: 8, source: 'tenant' })]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'parameters-row').map((row) => row.getAttribute('data-key'))).toEqual(['example.other']);
  });

  it('offers a retry when the list fails to load', async () => {
    await loadSession(role('admin'));

    const fixture = TestBed.createComponent(ParameterListPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'x' });
    await settle(fixture);

    expect(allByTestId(root(fixture), 'parameters-row')).toHaveLength(0);

    const retry = byTestId(root(fixture), 'parameters-retry');

    expect(retry).not.toBeNull();
    await click(fixture, retry);
    (await nextCall('GET', LIST_URL)).flush(pageOf([parameterView(KEY)]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'parameters-row').map((row) => row.getAttribute('data-key'))).toEqual([KEY]);
  });
});
