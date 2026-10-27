import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { TranslocoModule, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { GATBASE_CORE_I18N } from '../module';

/**
 * Shows a secret once (API key, invitation link) in a modal dialog that a click on the mask does not close.
 * The value lives only in this component's `secret` binding: the host clears its signal on `closed`.
 */
@Component({
  selector: 'app-one-time-secret-dialog',
  imports: [DialogModule, ButtonModule, InputTextModule, TranslocoModule],
  providers: [provideTranslocoScope(GATBASE_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './one-time-secret-dialog.component.html'
})
export class OneTimeSecretDialogComponent {
  /** Full translation key of the dialog title, such as `gatbaseCore.invitations.linkTitle`. */
  readonly titleKey = input.required<string>();
  /** The secret to show; null hides the dialog. */
  readonly secret = input.required<string | null>();
  readonly closed = output<void>();

  protected async copy(): Promise<void> {
    const value = this.secret();

    if (value === null) {
      return;
    }

    try {
      await navigator.clipboard.writeText(value);
    } catch {
      // The clipboard is unavailable (insecure context or denied permission); the value stays selectable in the field.
    }
  }

  protected close(): void {
    this.closed.emit();
  }

  protected onVisibleChange(isVisible: boolean): void {
    if (!isVisible) {
      this.closed.emit();
    }
  }
}
