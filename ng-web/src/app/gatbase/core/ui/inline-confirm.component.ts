import { ChangeDetectionStrategy, Component, input, output, signal } from '@angular/core';
import { TranslocoModule, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { GATBASE_CORE_I18N } from '../module';

/** A button that turns into Confirm / Cancel in place; emits `confirmed` once per confirmation. */
@Component({
  selector: 'app-inline-confirm',
  imports: [ButtonModule, TranslocoModule],
  providers: [provideTranslocoScope(GATBASE_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './inline-confirm.component.html'
})
export class InlineConfirmComponent {
  /** Full translation key of the first button. */
  readonly labelKey = input.required<string>();
  /** Test id of the first button; the other two are `<testId>-confirm` and `<testId>-cancel`. */
  readonly testId = input.required<string>();
  readonly disabled = input(false);
  readonly confirmed = output<void>();

  protected readonly isConfirming = signal(false);

  protected ask(): void {
    if (this.disabled()) {
      return;
    }

    this.isConfirming.set(true);
  }

  protected cancel(): void {
    this.isConfirming.set(false);
  }

  protected confirm(): void {
    if (!this.isConfirming() || this.disabled()) {
      return;
    }

    this.isConfirming.set(false);
    this.confirmed.emit();
  }
}
