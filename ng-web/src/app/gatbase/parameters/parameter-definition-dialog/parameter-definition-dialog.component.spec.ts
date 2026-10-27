import { HttpTestingController } from '@angular/common/http/testing';
import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { parameterView } from '../../../../testing/fixtures/parameter-view';
import { okEnvelope } from '../../../../testing/fixtures/session-view';
import type { ParameterView } from '../parameters.types';
import { ParameterDefinitionDialog } from './parameter-definition-dialog.component';

const LIST_URL = '/api/v1/parameters';

const KEY = 'example.retry.max_attempts';

@Component({
  selector: 'app-host',
  imports: [ParameterDefinitionDialog],
  template: `<app-parameter-definition-dialog [visible]="isVisible()" [parameter]="parameter()" (saved)="saved.push($event)" (closed)="isVisible.set(false)" />`
})
class HostComponent {
  readonly isVisible = signal(false);
  readonly parameter = signal<ParameterView | null>(null);
  readonly saved: ParameterView[] = [];
}

describe('ParameterDefinitionDialog', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    stubMatchMedia();
    configureFeatureTest();
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function open(existing: Record<string, unknown> | null = null): Promise<ComponentFixture<HostComponent>> {
    const fixture = TestBed.createComponent(HostComponent);

    await settle(fixture);
    fixture.componentInstance.parameter.set(existing === null ? null : (existing as unknown as ParameterView));
    fixture.componentInstance.isVisible.set(true);
    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(document.body)).toContain(existing === null ? 'Nuevo parámetro' : 'Editar parámetro');
    });

    return fixture;
  }

  function field(testId: string): HTMLInputElement {
    return byTestId(document.body, testId) as HTMLInputElement;
  }

  async function save(fixture: ComponentFixture<HostComponent>): Promise<void> {
    (byTestId(document.body, 'parameter-def-save') as HTMLButtonElement).click();
    await settle(fixture);
  }

  function pending(method: string, url: string) {
    const request = http.expectOne((candidate) => candidate.method === method && candidate.url === url);

    return { body: request.request.body, flush: (body: object, status = 200) => request.flush(body, { status, statusText: 'x' }) };
  }

  async function chooseType(fixture: ComponentFixture<HostComponent>, type: string): Promise<void> {
    field('parameter-def-type').click();
    await settle(fixture);

    const option = allByTestId(document.body, 'parameter-def-type-option').find((candidate) => candidate.getAttribute('data-key') === type);

    (option as HTMLElement).click();
    await settle(fixture);
  }

  async function fillNumberDefinition(fixture: ComponentFixture<HostComponent>): Promise<void> {
    typeInto(field('parameter-def-key'), KEY);
    await chooseType(fixture, 'number');
    typeInto(field('parameter-def-default'), '3');
    typeInto(field('parameter-def-min'), '1');
    typeInto(field('parameter-def-max'), '10');
    typeInto(field('parameter-def-description'), 'Retries');
    await settle(fixture);
  }

  it('creates a number definition with its bounds', async () => {
    const fixture = await open();

    await fillNumberDefinition(fixture);
    await save(fixture);

    const request = pending('POST', LIST_URL);

    expect(request.body).toEqual({ key: KEY, valueType: 'number', defaultValue: 3, minValue: 1, maxValue: 10, allowedValues: null, description: 'Retries', isSystemRequired: false });
    request.flush(okEnvelope(parameterView(KEY)), 201);
    await settle(fixture);

    expect(fixture.componentInstance.saved).toHaveLength(1);
    expect(fixture.componentInstance.isVisible()).toBe(false);
  });

  it('sends no bounds when the fields are empty and a parsed allowed list when given', async () => {
    const fixture = await open();

    typeInto(field('parameter-def-key'), 'example.mode');
    typeInto(field('parameter-def-default'), 'fast');
    typeInto(field('parameter-def-allowed'), '["fast","slow"]');
    await save(fixture);

    expect(pending('POST', LIST_URL).body).toEqual({ key: 'example.mode', valueType: 'string', defaultValue: 'fast', minValue: null, maxValue: null, allowedValues: ['fast', 'slow'], description: '', isSystemRequired: false });
  });

  it('sends a boolean default as the toggle holds it', async () => {
    const fixture = await open();

    typeInto(field('parameter-def-key'), 'example.flag');
    await chooseType(fixture, 'boolean');
    (field('parameter-def-default').querySelector('input') as HTMLInputElement).click();
    await settle(fixture);
    await save(fixture);

    const request = pending('POST', LIST_URL);

    expect(request.body).toMatchObject({ valueType: 'boolean', defaultValue: true });
    request.flush(okEnvelope(parameterView('example.flag')), 201);
    await settle(fixture);
  });

  it.each([
    ['a default that is not a number', 'parameter-def-default', 'abc', 'parameter-def-default-error', 'Ingresa un número.'],
    ['a default that is NaN', 'parameter-def-default', 'NaN', 'parameter-def-default-error', 'Ingresa un número.'],
    ['a default that is Infinity', 'parameter-def-default', 'Infinity', 'parameter-def-default-error', 'Ingresa un número.'],
    ['an empty default for a number', 'parameter-def-default', '', 'parameter-def-default-error', 'Ingresa un número.'],
    ['an allowed list that is not JSON', 'parameter-def-allowed', 'a,b', 'parameter-def-allowed-error', 'Ingresa una lista JSON, por ejemplo ["a", "b"].'],
    ['an allowed value that is not a list', 'parameter-def-allowed', '{"a":1}', 'parameter-def-allowed-error', 'Ingresa una lista JSON, por ejemplo ["a", "b"].']
  ])('shows a local error and sends nothing for %s', async (_name, input, text, errorId, message) => {
    const fixture = await open();

    await fillNumberDefinition(fixture);
    typeInto(field(input), text);
    await save(fixture);

    expect(textOf(byTestId(document.body, errorId))).toBe(message);
    http.expectNone(LIST_URL);
  });

  it('shows a local error and sends nothing for a json default that is not JSON', async () => {
    const fixture = await open();

    typeInto(field('parameter-def-key'), 'example.payload');
    await chooseType(fixture, 'json');
    typeInto(field('parameter-def-default'), '{not json');
    await save(fixture);

    expect(textOf(byTestId(document.body, 'parameter-def-default-error'))).toBe('El texto no es un JSON válido.');
    http.expectNone(LIST_URL);
  });

  it('maps 422 issues to the field they name', async () => {
    const fixture = await open();

    await fillNumberDefinition(fixture);
    await save(fixture);
    pending('POST', LIST_URL).flush(
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'key', code: 'PARAMETER_KEY_INVALID', message: 'x' },
        { field: 'minValue', code: 'PARAMETER_CONSTRAINT_INVALID', message: 'x' },
        { field: 'description', code: 'FIELD_TOO_LONG', message: 'x' }
      ]),
      422
    );
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-def-key-error'))).toContain('La clave no es válida');
    expect(textOf(byTestId(document.body, 'parameter-def-min-error'))).toContain('El mínimo, el máximo');
    expect(textOf(byTestId(document.body, 'parameter-def-description-error'))).toBe(es.validation.rules.FIELD_TOO_LONG);
    expect(byTestId(document.body, 'parameter-def-issues')).toBeNull();
  });

  it('shows PARAMETER_ALREADY_EXISTS under the key', async () => {
    const fixture = await open();

    await fillNumberDefinition(fixture);
    await save(fixture);
    pending('POST', LIST_URL).flush(errorEnvelope('PARAMETER_ALREADY_EXISTS'), 409);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-def-key-error'))).toBe('Ya existe un parámetro con esta clave.');
    expect(byTestId(document.body, 'parameter-def-issues')).toBeNull();
  });

  it('shows PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS as a form-level message', async () => {
    const fixture = await open({ ...parameterView(KEY) });

    typeInto(field('parameter-def-max'), '5');
    await save(fixture);
    pending('PUT', `${LIST_URL}/${KEY}`).flush(errorEnvelope('PARAMETER_OVERRIDES_VIOLATE_CONSTRAINTS'), 409);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-def-issues'))).toContain('romperían las nuevas restricciones');
    expect(byTestId(document.body, 'parameter-def-key-error')).toBeNull();
    expect(fixture.componentInstance.isVisible()).toBe(true);
  });

  it('opens an edit with key, type and system flag read-only and the stored values filled in', async () => {
    await open(parameterView(KEY, { isSystemRequired: true }));

    expect(field('parameter-def-key').readOnly).toBe(true);
    expect(field('parameter-def-key').value).toBe(KEY);
    expect(field('parameter-def-default').value).toBe('3');
    expect(field('parameter-def-min').value).toBe('1');
    expect(field('parameter-def-max').value).toBe('10');
    expect((byTestId(document.body, 'parameter-def-type') as HTMLElement).querySelector('[aria-disabled="true"]')).not.toBeNull();
    expect((byTestId(document.body, 'parameter-def-system') as HTMLElement).querySelector('input')?.disabled).toBe(true);
  });

  it('sends the stored immutable fields with an edit', async () => {
    const fixture = await open(parameterView(KEY, { isSystemRequired: true }));

    typeInto(field('parameter-def-default'), '4');
    await save(fixture);

    const request = pending('PUT', `${LIST_URL}/${KEY}`);

    expect(request.body).toEqual({ valueType: 'number', defaultValue: 4, minValue: 1, maxValue: 10, allowedValues: null, description: 'Synthetic parameter', isSystemRequired: true });
    request.flush(okEnvelope(parameterView(KEY, { defaultValue: 4 })));
  });

  it('shows PARAMETER_FIELD_IMMUTABLE under the type', async () => {
    const fixture = await open(parameterView(KEY));

    await save(fixture);
    pending('PUT', `${LIST_URL}/${KEY}`).flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'valueType', code: 'PARAMETER_FIELD_IMMUTABLE', message: 'x' }]), 422);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-def-type-error'))).toContain('no se puede cambiar');
  });

  it('starts empty each time it opens for a new definition', async () => {
    const fixture = await open();

    typeInto(field('parameter-def-key'), 'example.left_over');
    (byTestId(document.body, 'parameter-def-cancel') as HTMLButtonElement).click();
    await settle(fixture);
    fixture.componentInstance.isVisible.set(true);
    await settle(fixture);

    expect(field('parameter-def-key').value).toBe('');
  });
});
