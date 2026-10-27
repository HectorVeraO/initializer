import { Component, inject, ChangeDetectionStrategy } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { TranslocoModule } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { ConfirmDeleteService } from '../confirm-delete/confirm-delete.service';

@Component({
  selector: 'p-confirm-name-dialog',
  standalone: true,
  imports: [FormsModule, TranslocoModule, DialogModule, ButtonModule, InputTextModule],
  templateUrl: './confirm-name-dialog.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrl: './confirm-name-dialog.component.scss'
})
export class ConfirmNameDialogComponent {
  readonly svc = inject(ConfirmDeleteService);
  inputValue = '';

  onAccept(): void {
    if (!this.matchesConfirmText()) {
      return;
    }
    this.inputValue = '';
    this.svc.accept();
  }

  onCancel(): void {
    this.inputValue = '';
    this.svc.reject();
  }

  matchesConfirmText(): boolean {
    return this.inputValue.trim().toLocaleLowerCase('es-CL') === this.svc.confirmText();
  }
}
