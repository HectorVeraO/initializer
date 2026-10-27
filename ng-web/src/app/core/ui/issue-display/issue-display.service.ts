import { HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';
import { MessageService, ToastMessageOptions } from '@openng/optimus-ui/api';
import { apiIssues, errorMessageWithReference } from '@/core/api/api-error';
import type { ApiIssue } from '@/core/envelope';

/**
 * Presents the issues of an API error envelope (`error.issues[]`) to the user.
 *
 * Two strategies: a toast per issue (fire-and-forget), or
 * `ToastMessageOptions[]` that a view binds to inline `<p-message>` components.
 * Callers invoke it explicitly, so the interceptor layer stays thin.
 */
@Injectable({ providedIn: 'root' })
export class IssueDisplayService {
  private readonly messageService = inject(MessageService);
  private readonly transloco = inject(TranslocoService);

  showSuccessToast(summary: string, detail?: string): void {
    this.messageService.add({ severity: 'success', summary, detail, life: 3000 });
  }

  /** Shows each issue as an error toast. */
  showToast(issues: ApiIssue[]): void {
    for (const issue of issues) {
      this.messageService.add({ severity: 'error', summary: issue.code, detail: issue.message });
    }
  }

  /**
   * Displays the issues of a failed call as toasts; without issues, one
   * generic toast with the message of the error code and the trace reference.
   *
   * @returns `true` if issues were displayed, `false` for the generic toast.
   */
  showErrorToast(error: HttpErrorResponse): boolean {
    const issues = apiIssues(error);

    if (issues.length > 0) {
      this.showToast(issues);

      return true;
    }

    const detail = errorMessageWithReference(this.transloco, error);

    this.messageService.add({
      severity: 'error',
      summary: this.transloco.translate('errors.generic-error.summary'),
      detail,
      life: 4000
    });

    return false;
  }

  /** Converts issues into message options for inline `<p-message>` lists. */
  toMessageOptions(issues: ApiIssue[]): ToastMessageOptions[] {
    return issues.map((issue) => ({ severity: 'error', summary: issue.code, detail: issue.message }));
  }
}
