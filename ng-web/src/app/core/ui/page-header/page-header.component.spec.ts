import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { byTestId, textOf } from '@/core/testing/ui-testing';
import { PageHeaderComponent } from './page-header.component';

@Component({
  selector: 'p-page-header-host',
  standalone: true,
  imports: [PageHeaderComponent],
  template: `
    <p-page-header [title]="title()" [subtitle]="subtitle()" [titleTestId]="titleTestId()" [subtitleTestId]="subtitleTestId()">
      <button titleAction type="button" data-testid="title-action">Actualizar</button>
      <button type="button" data-testid="primary-action">Nuevo</button>
    </p-page-header>
  `
})
class HostComponent {
  readonly title = signal('Parámetros');
  readonly subtitle = signal('');
  readonly titleTestId = signal('');
  readonly subtitleTestId = signal('');
}

describe('PageHeaderComponent', () => {
  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  it('renders the title as the page heading and the projected actions', () => {
    const { root } = render();

    expect(textOf(root.querySelector('h1'))).toBe('Parámetros');
    expect(textOf(byTestId(root, 'title-action'))).toBe('Actualizar');
    expect(textOf(byTestId(root, 'primary-action'))).toBe('Nuevo');
  });

  it('shows the subtitle only when it is given', () => {
    const { root, host, refresh } = render();

    expect(root.querySelectorAll('p')).toHaveLength(0);

    host.subtitle.set('Parámetros de la plataforma.');
    refresh();

    expect(textOf(root.querySelector('p'))).toBe('Parámetros de la plataforma.');
  });

  it('puts the given test ids on the title and the subtitle', () => {
    const { root, host, refresh } = render();

    host.subtitle.set('Lead');
    host.titleTestId.set('the-title');
    host.subtitleTestId.set('the-subtitle');

    refresh();

    expect(textOf(byTestId(root, 'the-title'))).toBe('Parámetros');
    expect(byTestId(root, 'the-title')?.tagName).toBe('H1');
    expect(textOf(byTestId(root, 'the-subtitle'))).toBe('Lead');
  });

  it('adds no test id attribute when none is given', () => {
    const { root, host, refresh } = render();

    host.subtitle.set('Lead');

    refresh();

    expect(root.querySelector('h1')?.hasAttribute('data-testid')).toBe(false);
    expect(root.querySelector('p')?.hasAttribute('data-testid')).toBe(false);
  });
});
