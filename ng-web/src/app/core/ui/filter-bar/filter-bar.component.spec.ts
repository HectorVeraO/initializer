import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { byTestId, configureFeatureTest, textOf } from '@/core/testing/ui-testing';
import { FilterBarComponent } from './filter-bar.component';

@Component({
  selector: 'p-filter-bar-host',
  standalone: true,
  imports: [FilterBarComponent],
  template: `
    <p-filter-bar [clearable]="clearable()" [busy]="busy()" (apply)="searches.set(searches() + 1)" (clear)="clears.set(clears() + 1)">
      <input data-testid="filter-input" />
    </p-filter-bar>
  `
})
class HostComponent {
  readonly clearable = signal(false);
  readonly busy = signal(false);
  readonly searches = signal(0);
  readonly clears = signal(0);
}

describe('FilterBarComponent', () => {
  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    configureFeatureTest();

    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  it('shows the projected controls and a Buscar button that submits the form without reloading', () => {
    const { root, host } = render();

    const submit = new Event('submit', { cancelable: true });

    root.querySelector('form')?.dispatchEvent(submit);

    expect(byTestId(root, 'filter-input')).not.toBeNull();
    expect(textOf(byTestId(root, 'filters-apply'))).toBe('Buscar');
    expect(submit.defaultPrevented).toBe(true);
    expect(host.searches()).toBe(1);
  });

  it('offers Limpiar only when clearable and reports the click', () => {
    const { root, host, refresh } = render();

    expect(byTestId(root, 'filters-clear')).toBeNull();

    host.clearable.set(true);
    refresh();

    const clear = byTestId(root, 'filters-clear');

    expect(textOf(clear)).toBe('Limpiar');

    clear?.click();

    expect(host.clears()).toBe(1);
    expect(host.searches()).toBe(0);
  });

  it('disables both buttons while busy', () => {
    const { root, host, refresh } = render();

    host.clearable.set(true);
    host.busy.set(true);
    refresh();

    expect(byTestId(root, 'filters-apply')?.hasAttribute('disabled')).toBe(true);
    expect(byTestId(root, 'filters-clear')?.hasAttribute('disabled')).toBe(true);
  });
});
