import { HttpErrorResponse } from '@angular/common/http';
import { TranslocoService } from '@jsverse/transloco';
import type { ApiErrorBody, ApiIssue } from '@/core/envelope';
import { ApiErrorCode } from './api-error-codes';

export const DEFAULT_RULE_KEY = 'validation.rules.default';

/** `403` codes that send the person to the forbidden page. */
export const FORBIDDEN_PAGE_CODES: readonly string[] = [ApiErrorCode.InsufficientRole, ApiErrorCode.PlatformOperatorRequired];

const TRACE_ID_HEADER = 'X-Trace-Id';

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

/** True for a `403` whose code sends the person to the forbidden page. */
export function isForbiddenPageError(error: unknown): boolean {
  if (!(error instanceof HttpErrorResponse) || error.status !== 403) {
    return false;
  }

  const code = errorCode(error);

  return code !== null && FORBIDDEN_PAGE_CODES.includes(code);
}

/** False for a `GET`/`HEAD` `403` whose code is in FORBIDDEN_PAGE_CODES (the interceptor already navigated). */
export function showsLoadError(error: unknown): boolean {
  return !isForbiddenPageError(error);
}

/** Trace id of a failed call: `meta.traceId` of the envelope, else the `X-Trace-Id` header, else null. */
export function apiTraceId(error: unknown): string | null {
  if (!(error instanceof HttpErrorResponse)) {
    return null;
  }

  const body = parseBody(error.error) as { meta?: { traceId?: unknown } | null } | null;

  const fromBody = body?.meta?.traceId;

  if (typeof fromBody === 'string' && fromBody !== '') {
    return fromBody;
  }

  const fromHeader = error.headers?.get(TRACE_ID_HEADER) ?? null;

  return fromHeader !== null && fromHeader !== '' ? fromHeader : null;
}

/** Appends `errors.reference` with the trace id of a failed call to its text; the text alone when the call carries none. */
export function withTraceReference(transloco: TranslocoService, text: string, error: unknown): string {
  const traceId = apiTraceId(error);

  if (traceId === null) {
    return text;
  }

  const referenced = `${text} ${transloco.translate('errors.reference', { traceId })}`;

  return referenced;
}

/**
 * Text for a failed call: `errors.codes.<CODE>` when that key exists, `errors.operationFailed` for status 0 or 5xx without an envelope, else `errors.generic-error.detail`.
 * A failure the person cannot act on (status 0, 5xx, or a code without its own text) is followed by the trace reference, so support can find the server logs.
 */
export function errorMessage(transloco: TranslocoService, error: unknown): string {
  const isActionable = !isServerFailure(error) && translatedCode(transloco, error) !== null;

  const text = failureText(transloco, error);

  return isActionable ? text : withTraceReference(transloco, text, error);
}

/** Like `errorMessage`, but the trace reference follows every failure that carries a trace id. */
export function errorMessageWithReference(transloco: TranslocoService, error: unknown): string {
  const message = withTraceReference(transloco, failureText(transloco, error), error);

  return message;
}

function translatedCode(transloco: TranslocoService, error: unknown): string | null {
  const code = errorCode(error);

  if (code === null) {
    return null;
  }

  const key = `errors.codes.${code}`;

  const translated = transloco.translate(key);

  return translated !== key ? translated : null;
}

function failureText(transloco: TranslocoService, error: unknown): string {
  const translated = translatedCode(transloco, error);

  if (translated !== null) {
    return translated;
  }

  if (errorCode(error) === null && isServerFailure(error)) {
    return transloco.translate('errors.operationFailed');
  }

  return transloco.translate('errors.generic-error.detail');
}
