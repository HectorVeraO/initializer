import { HttpTestingController } from '@angular/common/http/testing';
import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { parameterView } from '../../../../testing/fixtures/parameter-view';
import { okEnvelope } from '../../../../testing/fixtures/session-view';
import type { ParameterView } from '../parameters.types';
import { ParameterOverrideDialog } from './parameter-override-dialog.component';

const KEY = 'example.retry.max_attempts';

const URL = `/api/v1/parameters/${KEY}/tenant-override`;

@Component({
  selector: 'app-host',
  imports: [ParameterOverrideDialog],
  template: `<app-parameter-override-dialog [parameter]="parameter()" (saved)="saved.push($event)" (closed)="parameter.set(null)" />`
})
class HostComponent {
  readonly parameter = signal<ParameterView | null>(null);
  readonly saved: ParameterView[] = [];
}

describe('ParameterOverrideDialog', () => {
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

  async function open(overrides: Record<string, unknown> = {}): Promise<ComponentFixture<HostComponent>> {
    const fixture = TestBed.createComponent(HostComponent);

    await settle(fixture);
    fixture.componentInstance.parameter.set(parameterView(KEY, overrides) as unknown as ParameterView);
    await settle(fixture);
    // The module translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(document.body)).toContain('Valor propio del tenant activo');
    });

    return fixture;
  }

  function editor(): HTMLElement {
    return byTestId(document.body, 'parameter-override-value') as HTMLElement;
  }

  function numberInput(): HTMLInputElement {
    return editor().querySelector('input') as HTMLInputElement;
  }

  async function save(fixture: ComponentFixture<HostComponent>): Promise<void> {
    (byTestId(document.body, 'parameter-override-save') as HTMLButtonElement).click();
    await settle(fixture);
  }

  function sent(): { body: unknown; flush: (body: object, status?: number) => void } {
    const request = http.expectOne((candidate) => candidate.method === 'PUT' && candidate.url === URL);

    return { body: request.request.body, flush: (body, status = 200) => request.flush(body, { status, statusText: 'x' }) };
  }

  function noRequest(): void {
    http.expectNone(URL);
  }

  it('starts a number editor from the value in effect', async () => {
    await open({ effectiveValue: 4 });

    expect(numberInput().value).toBe('4');
  });

  it.each([
    ['the maximum', '10', 10],
    ['the minimum', '1', 1],
    ['a value inside the bounds', '7', 7]
  ])('sends %s of a number parameter as it is', async (_name, typed, expected) => {
    const fixture = await open();

    typeInto(numberInput(), typed);
    numberInput().dispatchEvent(new Event('blur'));
    await save(fixture);

    const request = sent();

    expect(request.body).toEqual({ value: expected });
    request.flush(okEnvelope(parameterView(KEY, { effectiveValue: expected, source: 'tenant' })));
  });

  it('sends a cleared number editor as null and shows the refusal of the server', async () => {
    const fixture = await open({ effectiveValue: 4 });

    typeInto(numberInput(), '');
    numberInput().dispatchEvent(new Event('blur'));
    await save(fixture);

    const request = sent();

    expect(request.body).toEqual({ value: null });
    request.flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'value', code: 'PARAMETER_TYPE_MISMATCH', message: 'x' }]), 422);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-override-value-error'))).toBe('El valor no corresponde al tipo del parámetro.');
  });

  it('shows the out-of-range refusal of the server under the input', async () => {
    const fixture = await open();

    typeInto(numberInput(), '7');
    numberInput().dispatchEvent(new Event('blur'));
    await save(fixture);
    sent().flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'value', code: 'PARAMETER_OUT_OF_RANGE', message: 'x' }]), 422);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-override-value-error'))).toBe('El valor está fuera del rango permitido.');
    expect(byTestId(document.body, 'parameter-override-dialog')).not.toBeNull();
    expect(fixture.componentInstance.saved).toHaveLength(0);
  });

  it('shows the type and allowed-value refusals under the input', async () => {
    const fixture = await open();

    await save(fixture);
    sent().flush(
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'value', code: 'PARAMETER_TYPE_MISMATCH', message: 'x' },
        { field: 'value', code: 'PARAMETER_VALUE_NOT_ALLOWED', message: 'x' }
      ]),
      422
    );
    await settle(fixture);

    const texts = allByTestId(document.body, 'parameter-override-value-error').map((element) => textOf(element));

    expect(texts).toEqual(['El valor no corresponde al tipo del parámetro.', 'El valor no es uno de los valores permitidos.']);
  });

  it('offers a select when the parameter has allowed values and sends the chosen one', async () => {
    const fixture = await open({ valueType: 'string', effectiveValue: 'a', defaultValue: 'a', minValue: null, maxValue: null, allowedValues: ['a', 'b', 'c'] });

    editor().click();
    await settle(fixture);

    const options = allByTestId(document.body, 'parameter-override-option');

    expect(options.map((option) => textOf(option))).toEqual(['a', 'b', 'c']);
    options[1].click();
    await settle(fixture);
    await save(fixture);

    expect(sent().body).toEqual({ value: 'b' });
  });

  it('shows object options as JSON and sends the chosen object', async () => {
    const fixture = await open({ valueType: 'json', effectiveValue: { mode: 'a' }, defaultValue: { mode: 'a' }, minValue: null, maxValue: null, allowedValues: [{ mode: 'a' }, { mode: 'b' }] });

    editor().click();
    await settle(fixture);

    const options = allByTestId(document.body, 'parameter-override-option');

    expect(options.map((option) => textOf(option))).toEqual(['{"mode":"a"}', '{"mode":"b"}']);
    options[1].click();
    await settle(fixture);
    await save(fixture);

    expect(sent().body).toEqual({ value: { mode: 'b' } });
  });

  it('toggles a boolean parameter', async () => {
    const fixture = await open({ valueType: 'boolean', effectiveValue: false, defaultValue: false, minValue: null, maxValue: null });

    (editor().querySelector('input') as HTMLInputElement).click();
    await settle(fixture);
    await save(fixture);

    expect(sent().body).toEqual({ value: true });
  });

  it('sends the text of a string parameter as typed', async () => {
    const fixture = await open({ valueType: 'string', effectiveValue: 'old', defaultValue: 'old', minValue: null, maxValue: null });

    expect((editor() as HTMLInputElement).value).toBe('old');
    typeInto(editor() as HTMLInputElement, '  new value ');
    await save(fixture);

    expect(sent().body).toEqual({ value: '  new value ' });
  });

  it('sends an empty string for a string parameter left empty', async () => {
    const fixture = await open({ valueType: 'string', effectiveValue: 'old', defaultValue: 'old', minValue: null, maxValue: null });

    typeInto(editor() as HTMLInputElement, '');
    await save(fixture);

    expect(sent().body).toEqual({ value: '' });
  });

  it('sends the parsed value of a JSON parameter', async () => {
    const fixture = await open({ valueType: 'json', effectiveValue: { a: 1 }, defaultValue: {}, minValue: null, maxValue: null });

    expect((editor() as HTMLTextAreaElement).value).toBe('{"a":1}');
    typeInto(editor() as HTMLTextAreaElement, '{"list":[1,"two",null],"ok":true}');
    await save(fixture);

    expect(sent().body).toEqual({ value: { list: [1, 'two', null], ok: true } });
  });

  it.each([
    ['malformed text', '{"a":'],
    ['empty text', ''],
    ['blank text', '   ']
  ])('shows a local error and sends nothing for %s in a JSON parameter', async (_name, text) => {
    const fixture = await open({ valueType: 'json', effectiveValue: { a: 1 }, defaultValue: {}, minValue: null, maxValue: null });

    typeInto(editor() as HTMLTextAreaElement, text);
    await save(fixture);

    expect(textOf(byTestId(document.body, 'parameter-override-json-error'))).toBe('El texto no es un JSON válido.');
    noRequest();
  });

  it('clears the local JSON error as soon as the text changes', async () => {
    const fixture = await open({ valueType: 'json', effectiveValue: 1, defaultValue: 1, minValue: null, maxValue: null });

    typeInto(editor() as HTMLTextAreaElement, '{');
    await save(fixture);
    typeInto(editor() as HTMLTextAreaElement, '{}');
    await settle(fixture);

    expect(byTestId(document.body, 'parameter-override-json-error')).toBeNull();
  });

  it('emits the saved parameter and closes after a 200', async () => {
    const fixture = await open();

    typeInto(numberInput(), '5');
    numberInput().dispatchEvent(new Event('blur'));
    await save(fixture);
    sent().flush(okEnvelope(parameterView(KEY, { effectiveValue: 5, source: 'tenant' })));
    await settle(fixture);

    expect(fixture.componentInstance.saved.map((parameter) => parameter.effectiveValue)).toEqual([5]);
    expect(fixture.componentInstance.parameter()).toBeNull();
  });

  it('closes without a request on Cancel', async () => {
    const fixture = await open();

    (byTestId(document.body, 'parameter-override-cancel') as HTMLButtonElement).click();
    await settle(fixture);

    expect(fixture.componentInstance.parameter()).toBeNull();
    noRequest();
  });

  it('shows a form-level message on a server failure and keeps the dialog open', async () => {
    const fixture = await open();

    await save(fixture);
    sent().flush(errorEnvelope('INTERNAL_ERROR'), 500);
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'parameter-override-issues'))).toBe('Algo salió mal. Inténtalo de nuevo. Referencia: abc');
    expect(fixture.componentInstance.parameter()).not.toBeNull();
  });

  it('sends one request when Save is pressed twice before the answer', async () => {
    const fixture = await open();

    await save(fixture);
    await save(fixture);

    sent().flush(okEnvelope(parameterView(KEY)));
  });
});
