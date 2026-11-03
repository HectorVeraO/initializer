import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { allByTestId, textOf } from '@/core/testing/ui-testing';
import { FormFieldComponent } from './form-field.component';

@Component({
  selector: 'p-form-field-host',
  standalone: true,
  imports: [FormFieldComponent],
  template: `
    <p-form-field label="Nombre" controlId="name" [hint]="hint()" [errors]="errors()" errorTestId="name-error">
      <input id="name" />
    </p-form-field>
  `
})
class HostComponent {
  readonly hint = signal('');
  readonly errors = signal<string[]>([]);
}

describe('FormFieldComponent', () => {
  function render(): { root: HTMLElement; host: HostComponent; refresh: () => void } {
    TestBed.configureTestingModule({});

    const fixture = TestBed.createComponent(HostComponent);

    fixture.detectChanges();

    return { root: fixture.nativeElement as HTMLElement, host: fixture.componentInstance, refresh: () => fixture.detectChanges() };
  }

  it('puts the label above the control and ties it to the control id', () => {
    const { root } = render();

    const label = root.querySelector('label');

    expect(textOf(label)).toBe('Nombre');
    expect(label?.getAttribute('for')).toBe('name');
    expect(root.querySelector('#name')).not.toBeNull();
    expect(label?.compareDocumentPosition(root.querySelector('#name') as Element)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it('shows the hint only when there is one', () => {
    const { root, host, refresh } = render();

    expect(root.querySelector('small')).toBeNull();

    host.hint.set('Solo minusculas');
    refresh();

    expect(textOf(root.querySelector('small'))).toBe('Solo minusculas');
  });

  it('lists every error under the control with the given test id', () => {
    const { root, host, refresh } = render();

    host.errors.set(['Es obligatorio', 'Es muy corto']);
    refresh();

    expect(allByTestId(root, 'name-error').map((error) => textOf(error))).toEqual(['Es obligatorio', 'Es muy corto']);
  });
});
