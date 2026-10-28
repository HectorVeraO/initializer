import { Component, Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { provideOptimus } from '@openng/optimus-ui/config';
import { PLATFORM_MODULES } from '@/platform/core';
import type { TopbarItem, WebModule } from '@/platform/core';
import { byTestId, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import { AppTopbar } from './app.topbar';

@Component({ selector: 'p-probe-context', template: '<span data-testid="probe-context">context</span>' })
class ProbeContext {}

@Component({ selector: 'p-probe-account', template: '<span data-testid="probe-account">account</span>' })
class ProbeAccount {}

@Component({ selector: 'p-probe-other', template: '<span data-testid="probe-other">other</span>' })
class ProbeOther {}

function item(id: string, slot: TopbarItem['slot'], component: Type<unknown>): TopbarItem {
  return { id, slot, component };
}

function moduleWith(id: string, topbar?: readonly TopbarItem[]): WebModule {
  return { id, shellRoutes: [], publicRoutes: [], nav: [], i18n: { scope: id, alias: id, loader: { es: () => Promise.resolve({}) } }, topbar };
}

describe('AppTopbar', () => {
  beforeEach(() => {
    stubMatchMedia();
  });

  function render(modules: readonly WebModule[]): HTMLElement {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), provideOptimus({}), { provide: PLATFORM_MODULES, useValue: modules }]
    });

    const fixture = TestBed.createComponent(AppTopbar);

    fixture.detectChanges();

    return fixture.nativeElement as HTMLElement;
  }

  function slotIds(root: HTMLElement): string[] {
    const ids = Array.from(root.querySelectorAll('[data-testid^="topbar-"]'))
      .map((element) => element.getAttribute('data-testid') ?? '')
      .filter((id) => id !== 'topbar-logo');

    return ids;
  }

  it('renders a context item of a later module before an account item of an earlier one, each in its own wrapper', () => {
    const root = render([moduleWith('core', [item('account-probe', 'account', ProbeAccount)]), moduleWith('later', [item('context-probe', 'context', ProbeContext)])]);

    expect(slotIds(root)).toEqual(['topbar-context-probe', 'topbar-account-probe']);
    expect(textOf(byTestId(byTestId(root, 'topbar-context-probe') as HTMLElement, 'probe-context'))).toBe('context');
    expect(textOf(byTestId(byTestId(root, 'topbar-account-probe') as HTMLElement, 'probe-account'))).toBe('account');
  });

  it('keeps module order, then array order, within a slot', () => {
    const root = render([moduleWith('core', [item('first', 'context', ProbeContext), item('second', 'context', ProbeOther)]), moduleWith('later', [item('third', 'context', ProbeAccount)])]);

    expect(slotIds(root)).toEqual(['topbar-first', 'topbar-second', 'topbar-third']);
  });

  it('renders no slot item when no module has topbar items, and keeps the logo and the language and theme controls', () => {
    const root = render([moduleWith('core')]);

    expect(slotIds(root)).toEqual([]);
    expect(textOf(byTestId(root, 'topbar-logo'))).toBe('initializer');
    expect(byTestId(root, 'language-select')).not.toBeNull();
    expect(byTestId(root, 'theme-toggle')).not.toBeNull();
  });
});
