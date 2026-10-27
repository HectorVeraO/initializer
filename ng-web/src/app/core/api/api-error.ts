import { HttpErrorResponse } from '@angular/common/http';
import { TranslocoService } from '@jsverse/transloco';
import type { ApiErrorBody, ApiIssue } from '@/core/envelope';

export const DEFAULT_RULE_KEY = 'validation.rules.default';

/** Error code of a `403` that sends the person to the forbidden page. */
export const PERMISSION_DENIED = 'PERMISSION_DENIED';

interface ErrorBody {
  error?: Partial<ApiErrorBody> | null;
}

/** A call made with `responseType: 'text'` leaves its JSON error body as a string. */
function parseBody(raw: unknown): ErrorBody | null {
  if (typeof raw !== 'string') {
    return raw as ErrorBody | null;
  }

  try {
    return JSON.parse(raw) as ErrorBody | null;
  } catch {
    return null;
  }
}

function envelopeOf(error: unknown): Partial<ApiErrorBody> | null {
  if (!(error instanceof HttpErrorResponse)) {
    return null;
  }

  const body = parseBody(error.error);

  const failure = body?.error;

  if (failure === undefined || failure === null || typeof failure !== 'object') {
    return null;
  }

  return failure;
}

/** Issues of the envelope; empty when there are none or the body is not the envelope. */
export function apiIssues(error: unknown): ApiIssue[] {
  const issues = envelopeOf(error)?.issues;

  if (!Array.isArray(issues)) {
    return [];
  }

  const valid = issues.filter((issue) => typeof issue?.field === 'string' && typeof issue?.code === 'string');

  return valid;
}

/** The code of the first issue reported for one field, or null. */
export function fieldIssueCode(error: unknown, field: string): string | null {
  const match = apiIssues(error).find((issue) => issue.field === field);

  return match?.code ?? null;
}

/** True when the failure is the server being unreachable or failing (status 0 or 5xx). */
export function isServerFailure(error: unknown): boolean {
  if (!(error instanceof HttpErrorResponse)) {
    return false;
  }

  return error.status === 0 || error.status >= 500;
}

/** Translation of an issue code; a code without its own key falls back to the generic message. */
export function validationMessage(transloco: TranslocoService, code: string): string {
  const key = `validation.rules.${code}`;

  const translated = transloco.translate(key);

  if (translated !== key) {
    return translated;
  }

  return transloco.translate(DEFAULT_RULE_KEY);
}

/** Escapes a key or code for use as one path segment. */
export function pathSegment(value: string): string {
  return encodeURIComponent(value);
}

/** Envelope `error.code` of a failed call, or null. */
export function errorCode(error: unknown): string | null {
  const code = envelopeOf(error)?.code;

  return typeof code === 'string' ? code : null;
}

/** False for a `403 PERMISSION_DENIED`, which the interceptor already turns into the forbidden page. */
export function showsLoadError(error: unknown): boolean {
  if (!(error instanceof HttpErrorResponse)) {
    return true;
  }

  return !(error.status === 403 && errorCode(error) === PERMISSION_DENIED);
}
