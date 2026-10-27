import { TranslocoService } from '@jsverse/transloco';
import { validationMessage } from '@/core/api/api-error';
import type { ApiIssue } from '@/core/envelope';

/** Message for an issue or top-level code: `<scopeAlias>.issues.<code>` when the module scope has it, else the base `validation.rules.<code>` (`validation.rules.default` for an unknown code). */
export function issueMessage(transloco: TranslocoService, scopeAlias: string, code: string): string {
  const key = `${scopeAlias}.issues.${code}`;

  const translated = transloco.translate(key);

  if (translated !== key) {
    return translated;
  }

  return validationMessage(transloco, code);
}

/** True when `issue` belongs to `field` (`scopes[2]` counts as `scopes`). */
function isIssueOf(issue: ApiIssue, field: string): boolean {
  return issue.field === field || issue.field.startsWith(`${field}[`);
}

/** Messages of the issues of one field, through `issueMessage`. */
export function fieldIssueMessages(transloco: TranslocoService, scopeAlias: string, issues: readonly ApiIssue[], field: string): string[] {
  const messages = issues.filter((issue) => isIssueOf(issue, field)).map((issue) => issueMessage(transloco, scopeAlias, issue.code));

  return messages;
}

/** Messages of the issues that belong to none of `fields`, for the form-level list. */
export function unmatchedIssueMessages(transloco: TranslocoService, scopeAlias: string, issues: readonly ApiIssue[], fields: readonly string[]): string[] {
  const messages = issues.filter((issue) => !fields.some((field) => isIssueOf(issue, field))).map((issue) => issueMessage(transloco, scopeAlias, issue.code));

  return messages;
}
