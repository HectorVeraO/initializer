import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { configureFeatureTest, textOf } from '@/core/testing/ui-testing';
import { StatusTagComponent } from './status-tag.component';
import { StatusFamily } from './status-severity';

@Component({
  selector: 'p-status-tag-host',
  standalone: true,
  imports: [StatusTagComponent],
  template: `<p-status-tag [family]="family()" [value]="value()" data-testid="tag" />`
})
class HostComponent {
  readonly family = signal<StatusFamily>('membershipStatus');
  readonly value = signal<string | boolean | null>('active');
}

describe('StatusTagComponent', () => {
  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    configureFeatureTest();

    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  /** The tag itself, addressed by the raw value it renders for tests. */
  function tag(root: HTMLElement): HTMLElement | null {
    return root.querySelector<HTMLElement>('[data-value]');
  }

  it('labels every value of a family', () => {
    const { root, host, refresh } = render();

    const labels: Record<string, string> = { active: 'Activo', suspended: 'Suspendido', revoked: 'Revocado' };

    for (const [value, label] of Object.entries(labels)) {
      host.value.set(value);
      refresh();

      expect(textOf(root)).toBe(label);
    }
  });

  it('gives each value its severity and keeps the raw value for tests', () => {
    const { root, host, refresh } = render();

    expect(tag(root)?.getAttribute('data-severity')).toBe('success');
    expect(tag(root)?.getAttribute('data-value')).toBe('active');

    host.value.set('revoked');
    refresh();

    expect(textOf(root)).toBe('Revocado');
    expect(tag(root)?.getAttribute('data-severity')).toBe('danger');
  });

  it.each([
    ['tenantStatus', 'suspended', 'Suspendido', 'warn'],
    ['invitationStatus', 'pending', 'Pendiente', 'secondary'],
    ['invitationStatus', 'accepted', 'Aceptado', 'success'],
    ['invitationStatus', 'expired', 'Vencido', 'warn'],
    ['apiKeyStatus', 'revoked', 'Revocado', 'danger']
  ])('shows the %s value %s with its label and severity', (family, value, label, severity) => {
    const { root, host, refresh } = render();

    host.family.set(family as StatusFamily);
    host.value.set(value);
    refresh();

    expect(textOf(root)).toBe(label);
    expect(tag(root)?.getAttribute('data-severity')).toBe(severity);
  });

  it('reads the active flag as a boolean and labels both states', () => {
    const { root, host, refresh } = render();

    host.family.set('active');
    host.value.set(true);
    refresh();

    expect(textOf(root)).toBe('Activo');

    host.value.set(false);
    refresh();

    expect(textOf(root)).toBe('Inactivo');
    expect(tag(root)?.getAttribute('data-severity')).toBe('secondary');
  });

  it('keeps an unknown value as raw text on a neutral tag and shows a dash for null', () => {
    const { root, host, refresh } = render();

    host.value.set('SOMETHING_NEW');
    refresh();

    expect(textOf(root)).toBe('SOMETHING_NEW');
    expect(tag(root)?.getAttribute('data-severity')).toBe('secondary');

    host.value.set(null);
    refresh();

    expect(tag(root)).toBeNull();
    expect(textOf(root)).toBe('—');
  });
});
