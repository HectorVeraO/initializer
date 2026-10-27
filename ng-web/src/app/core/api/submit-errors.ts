import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';
import type { ApiIssue } from '@/core/envelope';
import { apiIssues, apiTraceId, errorCode, validationMessage } from './api-error';

/**
 * What a failed write left on screen: the envelope code, the issues of a
 * `422` and whether the server was unreachable or failing. Forms read it to
 * show messages next to their fields while keeping what the person typed.
 */
export class SubmitErrors {
  readonly status = signal<number | null>(null);
  readonly code = signal<string | null>(null);
  readonly issues = signal<ApiIssue[]>([]);
  readonly traceId = signal<string | null>(null);

  clear(): void {
    this.status.set(null);
    this.code.set(null);
    this.issues.set([]);
    this.traceId.set(null);
  }

  record(error: unknown): void {
    if (!(error instanceof HttpErrorResponse)) {
      this.status.set(0);
      this.code.set(null);
      this.issues.set([]);
      this.traceId.set(null);

      return;
    }

    this.status.set(error.status);
    this.code.set(errorCode(error));
    this.issues.set(apiIssues(error));
    this.traceId.set(apiTraceId(error));
  }

  /** True for `0` and `5xx`: the form stays open and shows the load-failure message. */
  serverFailed(): boolean {
    const status = this.status();

    return status !== null && (status === 0 || status >= 500);
  }

  /** True when the write answered `409` with the given envelope code. */
  conflictWith(code: string): boolean {
    return this.status() === 409 && this.code() === code;
  }

  /** Messages of the issues of one field (`scopes[2]` counts as `scopes`). */
  messagesFor(transloco: TranslocoService, field: string): string[] {
    const messages = this.issues()
      .filter((issue) => issue.field === field || issue.field.startsWith(`${field}[`))
      .map((issue) => validationMessage(transloco, issue.code));

    return messages;
  }
}
