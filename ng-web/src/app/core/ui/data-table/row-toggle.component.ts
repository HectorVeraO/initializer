import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { ButtonModule } from '@openng/optimus-ui/button';

/**
 * The open/close button of an expandable table row: a chevron that points down
 * while the row is open. Put it in the first cell of the row and wire `toggled`
 * to the `toggle` of the row context of `p-data-table`; `label` is the
 * accessible name, already translated by the caller (it names the row).
 */
@Component({
  selector: 'p-row-toggle',
  standalone: true,
  imports: [ButtonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button pButton type="button" severity="secondary" [text]="true" size="small" [attr.aria-label]="label()" [attr.aria-expanded]="expanded()" [attr.data-testid]="testId() || null" (click)="toggled.emit()">
      <i pButtonIcon class="pi" [class.pi-chevron-down]="expanded()" [class.pi-chevron-right]="!expanded()"></i>
    </button>
  `
})
export class RowToggleComponent {
  readonly expanded = input(false);

  readonly label = input('');

  /** `data-testid` of the button; '' leaves it out. */
  readonly testId = input('');

  readonly toggled = output<void>();
}
