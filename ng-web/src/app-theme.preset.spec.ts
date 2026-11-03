import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideOptimus } from '@openng/optimus-ui/config';
import { AppPreset } from './app-theme.preset';

@Component({ selector: 'p-theme-host', standalone: true, template: '' })
class ThemeHostComponent {}

/** Every rule the preset injects into the document, as one text. */
function injectedThemeCss(): string {
  return Array.from(document.head.querySelectorAll('style'))
    .map((style) => style.textContent ?? '')
    .join('\n');
}

describe('AppPreset', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [ThemeHostComponent], providers: [provideOptimus({ theme: { preset: AppPreset, options: { darkModeSelector: '.app-dark' } } })] });

    TestBed.createComponent(ThemeHostComponent).detectChanges();
  });

  it('exposes the success and danger scales the widgets read through Tailwind', () => {
    const css = injectedThemeCss();

    for (const variable of ['--p-success-50', '--p-success-500', '--p-success-600', '--p-success-700', '--p-danger-50', '--p-danger-600', '--p-danger-700']) {
      expect(css).toContain(`${variable}:`);
    }
  });
});
