import { ErrorHandler, inject, Injectable, Injector } from '@angular/core';
import { MessageService } from '@openng/optimus-ui/api';

type Severity = 'success' | 'info' | 'warn' | 'error';

interface ErrorHandlerMessage {
  severity: Severity;
  summary: string;
  detail: string;
}

@Injectable()
export class GlobalErrorHandler implements ErrorHandler {
  private readonly injector = inject(Injector);

  handleError(error: unknown): void {
    const messageService = this.injector.get(MessageService);

    console.error(error);

    if (this.hasNotificationRequest(error)) {
      const { severity, summary, detail } = error.notification;

      messageService.add({
        severity: severity,
        summary: summary,
        detail
      });
    }
  }

  private hasNotificationRequest(error: unknown): error is { notification: ErrorHandlerMessage } {
    return error !== null && typeof error === 'object' && 'notification' in error && typeof (error as Record<string, unknown>)['notification'] === 'object';
  }
}
