import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, TestRequest, provideHttpClientTesting } from '@angular/common/http/testing';
import { Component, Provider, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Routes, provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { ConfirmationService } from '@openng/optimus-ui/api';
import { provideOptimus } from '@openng/optimus-ui/config';
import es from '../../../assets/i18n/es.json';

/** Empty routed component for specs that need a route target. */
@Component({ selector: 'p-blank', template: '' })
export class BlankComponent {}

/** Test module shared by the feature specs: Spanish translations and HTTP boundary doubles. */
export function configureFeatureTest(routes: Routes = [], extraProviders: Provider[] = []): void {
  TestBed.configureTestingModule({
    imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
    providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter(routes), provideOptimus({}), ConfirmationService, ...extraProviders]
  });
}

export function httpController(): HttpTestingController {
  return TestBed.inject(HttpTestingController);
}

/** Creates the component and lets pending change detection settle. */
export async function render<T>(component: Type<T>): Promise<ComponentFixture<T>> {
  const fixture = TestBed.createComponent(component);

  fixture.detectChanges();
  await fixture.whenStable();

  return fixture;
}

export async function settle<T>(fixture: ComponentFixture<T>): Promise<void> {
  fixture.detectChanges();
  await fixture.whenStable();
  fixture.detectChanges();
}

export function byTestId(root: ParentNode, testId: string): HTMLElement | null {
  return root.querySelector<HTMLElement>(`[data-testid="${testId}"]`);
}

/** Text of each cell of a table row, separated by one space. */
export function rowText(row: Element | null): string {
  const cells = Array.from(row?.querySelectorAll('td, th') ?? []);

  return cells.map((cell) => textOf(cell)).join(' ');
}

export function allByTestId(root: ParentNode, testId: string): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(`[data-testid="${testId}"]`));
}

export function textOf(element: Element | null): string {
  return element?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
}

/** Types into an input as a person would: sets the value and fires `input`. */
export function typeInto(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

/**
 * Presses one printable key like a browser: keydown and keypress, and only if
 * neither was cancelled the character reaches the field and fires `input`.
 */
export function pressKey(input: HTMLInputElement, character: string): void {
  const code = character.charCodeAt(0);

  const init = { key: character, keyCode: code, which: code, charCode: code, bubbles: true, cancelable: true };

  const down = new KeyboardEvent('keydown', init);

  input.dispatchEvent(down);

  const press = new KeyboardEvent('keypress', init);

  input.dispatchEvent(press);

  if (down.defaultPrevented || press.defaultPrevented) {
    return;
  }

  typeInto(input, input.value + character);
}

/** Navigates to `url` with the router and returns the fixture of the routed component. */
export async function renderRoute<T>(url: string, component: Type<T>): Promise<ComponentFixture<T>> {
  const harness = await RouterTestingHarness.create();

  await harness.navigateByUrl(url, component);

  return harness.fixture as ComponentFixture<T>;
}

/** Types the keyword of the delete confirmation and presses its accept button. */
export async function confirmDeleteWithKeyword<T>(fixture: ComponentFixture<T>, keyword: string): Promise<void> {
  const dialog = document.body.querySelector('.confirm-delete-dialog') as HTMLElement;

  const input = dialog.querySelector('input') as HTMLInputElement;

  typeInto(input, keyword);
  await settle(fixture);

  const accept = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) => textOf(button) === 'Eliminar') as HTMLButtonElement;

  accept.click();
  await settle(fixture);
}

/** The accept button of the open delete confirmation. */
export function deleteConfirmAccept(): HTMLButtonElement {
  const dialog = document.body.querySelector('.confirm-delete-dialog') as HTMLElement;

  return Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) => textOf(button) === 'Eliminar') as HTMLButtonElement;
}

export function cancelDeleteConfirm(): void {
  const dialog = document.body.querySelector('.confirm-delete-dialog') as HTMLElement;

  const cancel = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) => textOf(button) === 'Cancelar') as HTMLButtonElement;

  cancel.click();
}

/** jsdom has no `matchMedia`; the overlays of the UI library ask for it when they open. */
export function stubMatchMedia(): void {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string) => ({ matches: false, media: query, onchange: null, addEventListener: () => undefined, removeEventListener: () => undefined, addListener: () => undefined, removeListener: () => undefined, dispatchEvent: () => false })
  });
}

/** The one pending request with this method and path (query string left out); fails when there is none or several. */
export function expectCall(method: string, path: string): TestRequest {
  return httpController().expectOne((request) => request.method === method && request.url === path);
}

/** Asserts that no request with this method and path is pending. */
export function expectNoCall(method: string, path: string): void {
  httpController().expectNone((request) => request.method === method && request.url === path);
}

/** jsdom has no canvas; the chart library asks for a 2D context when it draws. A null context makes it skip drawing. */
export function stubCanvas(): void {
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', { writable: true, configurable: true, value: () => null });
}
