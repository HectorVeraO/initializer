import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideOptimus } from '@openng/optimus-ui/config';
import { textOf } from '@/core/testing/ui-testing';
import { KpiTileComponent, KpiTone } from './kpi-tile.component';

@Component({
  selector: 'p-kpi-host',
  standalone: true,
  imports: [KpiTileComponent],
  template: `
    <p-kpi-tile label="Tasa de exito" [value]="value()" [tone]="tone()">
      <a href="/detail">Ver detalle</a>
    </p-kpi-tile>
  `
})
class HostComponent {
  readonly value = signal<string | number | null>(10788);
  readonly tone = signal<KpiTone>('success');
}

describe('KpiTileComponent', () => {
  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  it('shows the label, the value and the projected link', () => {
    const { root } = render();

    const parts = Array.from(root.querySelectorAll('p-kpi-tile span, p-kpi-tile a')).map((node) => textOf(node));

    expect(parts).toEqual(['Tasa de exito', '10788', 'Ver detalle']);
  });

  it('exposes its tone for styling', () => {
    const { root, host, refresh } = render();

    expect(root.querySelector('[data-tone]')?.getAttribute('data-tone')).toBe('success');

    host.tone.set('danger');
    refresh();

    expect(root.querySelector('[data-tone]')?.getAttribute('data-tone')).toBe('danger');
  });

  it('tags the value element with the given test id', () => {
    TestBed.configureTestingModule({ imports: [KpiTileComponent] });

    const fixture = TestBed.createComponent(KpiTileComponent);

    fixture.componentRef.setInput('label', 'Hallazgos');
    fixture.componentRef.setInput('value', 48);
    fixture.componentRef.setInput('valueTestId', 'findings-value');
    fixture.detectChanges();

    expect(textOf((fixture.nativeElement as HTMLElement).querySelector('[data-testid="findings-value"]'))).toBe('48');
  });

  it('hides the value and marks itself busy while loading', () => {
    TestBed.configureTestingModule({ imports: [KpiTileComponent], providers: [provideOptimus({})] });

    const fixture = TestBed.createComponent(KpiTileComponent);

    fixture.componentRef.setInput('label', 'Hallazgos');
    fixture.componentRef.setInput('value', 48);
    fixture.componentRef.setInput('loading', true);
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;

    expect(textOf(element)).not.toContain('48');
    expect(element.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('shows an em dash while the value is unknown', () => {
    const { root, host, refresh } = render();

    host.value.set(null);
    refresh();

    const parts = Array.from(root.querySelectorAll('p-kpi-tile span')).map((node) => textOf(node));

    expect(parts).toEqual(['Tasa de exito', '\u2014']);
  });
});
