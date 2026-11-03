import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideOptimus } from '@openng/optimus-ui/config';
import { textOf } from '@/core/testing/ui-testing';
import { SectionCardComponent, SectionCardTone } from './section-card.component';

@Component({
  selector: 'p-section-card-host',
  standalone: true,
  imports: [SectionCardComponent],
  template: `
    <p-section-card title="Top 5 mayor alza" [subtitle]="subtitle()" [icon]="icon()" [iconTone]="tone()">
      <span cardActions data-testid="card-badge">3 cadenas</span>
      <p data-testid="card-body">Sin variaciones</p>
    </p-section-card>
  `
})
class HostComponent {
  readonly subtitle = signal('Comparando 2026-05-21 vs 2026-05-22');
  readonly icon = signal('pi pi-arrow-up');
  readonly tone = signal<SectionCardTone>('success');
}

describe('SectionCardComponent', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideOptimus({})] });
  });

  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  it('renders the title, subtitle, header actions and body', () => {
    const { root } = render();

    expect(textOf(root.querySelector('h2'))).toBe('Top 5 mayor alza');
    expect(textOf(root)).toContain('Comparando 2026-05-21 vs 2026-05-22');
    expect(textOf(root.querySelector('[data-testid="card-badge"]'))).toBe('3 cadenas');
    expect(textOf(root.querySelector('[data-testid="card-body"]'))).toBe('Sin variaciones');
  });

  it('shows the icon tile with its tone only when an icon is given', () => {
    const { root, host, refresh } = render();

    expect(root.querySelector('[data-tone]')?.getAttribute('data-tone')).toBe('success');

    host.icon.set('');
    refresh();

    expect(root.querySelector('[data-tone]')).toBeNull();
  });

  it('omits the subtitle line when there is none', () => {
    const { root, host, refresh } = render();

    host.subtitle.set('');
    refresh();

    expect(textOf(root)).not.toContain('Comparando');
  });
});
