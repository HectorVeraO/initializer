import { Component, inject, ChangeDetectionStrategy } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MessageService, ToastMessageOptions } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { ToastModule } from '@openng/optimus-ui/toast';

@Component({
  selector: 'p-messages-demo',
  standalone: true,
  imports: [ToastModule, ButtonModule, InputTextModule, MessageModule, FormsModule],
  templateUrl: './messagesdemo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [MessageService]
})
export class MessagesDemo {
  msgs: ToastMessageOptions[] | null = [];

  username: string | undefined;

  email: string | undefined;

  private service = inject(MessageService);

  showInfoViaToast() {
    this.service.add({ severity: 'info', summary: 'Info Message', detail: 'Optimus UI rocks' });
  }

  showWarnViaToast() {
    this.service.add({ severity: 'warn', summary: 'Warn Message', detail: 'There are unsaved changes' });
  }

  showErrorViaToast() {
    this.service.add({ severity: 'error', summary: 'Error Message', detail: 'Validation failed' });
  }

  showSuccessViaToast() {
    this.service.add({ severity: 'success', summary: 'Success Message', detail: 'Message sent' });
  }
}
