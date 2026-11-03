import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * Label above a projected control, with an optional hint and one line per
 * error message under it. Used by filter bars, dialogs and forms so every
 * field reads the same. `controlId` must be the `id` of the projected control;
 * `errorTestId` tags every error line. Size the field with a class on the host.
 */
@Component({
  selector: 'p-form-field',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block min-w-0' },
  template: `
    <div class="flex flex-col gap-1.5">
      <label class="text-sm text-color" [attr.for]="controlId() || null">{{ label() }}</label>
      <ng-content />
      @if (hint()) {
        <small class="text-xs text-muted-color">{{ hint() }}</small>
      }
      @for (message of errors(); track message) {
        <small class="text-xs text-danger-600 dark:text-danger-400" role="alert" [attr.data-testid]="errorTestId() || null">{{ message }}</small>
      }
    </div>
  `
})
export class FormFieldComponent {
  readonly label = input.required<string>();
  readonly controlId = input('');
  readonly hint = input('');
  readonly errors = input<readonly string[]>([]);
  readonly errorTestId = input('');
}
