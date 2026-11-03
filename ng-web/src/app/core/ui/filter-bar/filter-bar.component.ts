import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { TranslocoModule } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { CardModule } from '@openng/optimus-ui/card';

/**
 * Filter bar of a list or report page: a card with a form whose projected
 * controls sit in one wrapping row (each with its label above, see
 * `p-form-field`), then "Limpiar" (only when `clearable`) and "Buscar" on the
 * right. Enter in any field submits. `apply` fires on submit, `clear` on
 * "Limpiar"; the page owns both behaviors. The submit button keeps the
 * `filters-apply` test id by default.
 */
@Component({
  selector: 'p-filter-bar',
  standalone: true,
  imports: [TranslocoModule, ButtonModule, CardModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block min-w-0' },
  template: `
    <p-card>
      <form class="flex flex-wrap items-end gap-3" (submit)="onSubmit($event)">
        <ng-content />
        <div class="flex items-center gap-2 sm:ml-auto">
          @if (clearable()) {
            <button pButton type="button" severity="secondary" [attr.data-testid]="clearTestId()" [disabled]="busy()" (click)="clear.emit()">
              <i pButtonIcon class="pi pi-times"></i>
              <span pButtonLabel>{{ 'filterBar.clear' | transloco }}</span>
            </button>
          }
          <button pButton type="submit" [attr.data-testid]="applyTestId()" [disabled]="busy()">
            <i pButtonIcon class="pi pi-search"></i>
            <span pButtonLabel>{{ 'filterBar.search' | transloco }}</span>
          </button>
        </div>
      </form>
    </p-card>
  `
})
export class FilterBarComponent {
  readonly clearable = input(false);
  readonly busy = input(false);
  readonly applyTestId = input('filters-apply');
  readonly clearTestId = input('filters-clear');

  readonly apply = output<void>();
  readonly clear = output<void>();

  protected onSubmit(event: Event): void {
    event.preventDefault();
    this.apply.emit();
  }
}
