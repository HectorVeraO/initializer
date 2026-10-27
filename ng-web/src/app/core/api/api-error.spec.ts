import { HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { TranslocoService, TranslocoTestingModule } from '@jsverse/transloco';
import en from '../../../assets/i18n/en.json';
import es from '../../../assets/i18n/es.json';
import { ApiErrorCode, IssueCode } from './api-error-codes';
import { FORBIDDEN_PAGE_CODES, apiIssues, apiTraceId, errorCode, errorMessage, fieldIssueCode, isServerFailure, showsLoadError, validationMessage } from './api-error';
import { errorEnvelope as envelope } from '../../../testing/fixtures/api-envelope';
import { SubmitErrors } from './submit-errors';

function failure(status: number, body: unknown): HttpErrorResponse {
  return new HttpErrorResponse({ status, error: body });
}

function configureTranslations(): TranslocoService {
  TestBed.configureTestingModule({ imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })] });

  return TestBed.inject(TranslocoService);
}

describe('api error envelope', () => {
  it('reads the code and the issues of a 422', () => {
    const error = failure(422, envelope('VALIDATION_FAILED', [{ field: 'name', code: 'required', message: 'required' }]));

    expect(errorCode(error)).toBe('VALIDATION_FAILED');
    expect(apiIssues(error)).toEqual([{ field: 'name', code: 'required', message: 'required' }]);
    expect(fieldIssueCode(error, 'name')).toBe('required');
    expect(fieldIssueCode(error, 'other')).toBeNull();
  });

  it('answers no issues for an envelope without them', () => {
    expect(apiIssues(failure(409, { error: { code: 'CONFLICT', message: 'exists' } }))).toEqual([]);
    expect(apiIssues(failure(409, envelope('CONFLICT')))).toEqual([]);
  });

  it('drops issues that are not field and code pairs', () => {
    const error = failure(422, envelope('VALIDATION_FAILED', [{ field: 'a' }, { field: 'b', code: 'max', message: 'm' }, 'x']));

    expect(apiIssues(error)).toEqual([{ field: 'b', code: 'max', message: 'm' }]);
  });

  it('survives a body that is not the envelope', () => {
    expect(errorCode(failure(502, '<html>Bad Gateway</html>'))).toBeNull();
    expect(apiIssues(failure(502, '<html>Bad Gateway</html>'))).toEqual([]);
    expect(errorCode(failure(500, null))).toBeNull();
    expect(apiIssues(failure(500, null))).toEqual([]);
    expect(errorCode(new Error('plain'))).toBeNull();
  });

  it('reads the envelope of a call made with a text response type, where the body is a JSON string', () => {
    const error = failure(403, JSON.stringify(envelope('INSUFFICIENT_ROLE')));

    expect(errorCode(error)).toBe('INSUFFICIENT_ROLE');
  });

  it('tells a server failure (status 0 or 5xx) from a client error', () => {
    expect(isServerFailure(failure(0, null))).toBe(true);
    expect(isServerFailure(failure(503, null))).toBe(true);
    expect(isServerFailure(failure(409, null))).toBe(false);
    expect(isServerFailure(failure(422, null))).toBe(false);
    expect(isServerFailure('nope')).toBe(false);
  });

  it('hides the load error only for a 403 that sends the person to the forbidden page', () => {
    expect(showsLoadError(failure(403, envelope('INSUFFICIENT_ROLE')))).toBe(false);
    expect(showsLoadError(failure(403, envelope('PLATFORM_OPERATOR_REQUIRED')))).toBe(false);
    expect(showsLoadError(failure(403, envelope('TENANT_SUSPENDED')))).toBe(true);
    expect(showsLoadError(failure(409, envelope('INSUFFICIENT_ROLE')))).toBe(true);
    expect(showsLoadError(failure(503, null))).toBe(true);
    expect(showsLoadError('nope')).toBe(true);
  });

  it('keeps an unknown error code readable as a string', () => {
    expect(errorCode(failure(409, envelope('SOME_FUTURE_CODE')))).toBe('SOME_FUTURE_CODE');
  });
});

describe('api error codes', () => {
  it('lists the 34 top-level codes of the registry once each', () => {
    const values = Object.values(ApiErrorCode);

    expect(values).toHaveLength(34);
    expect(new Set(values).size).toBe(34);
  });

  it('lists the 10 core issue codes of the registry', () => {
    expect(Object.values(IssueCode)).toEqual(['BODY_MALFORMED', 'FIELD_REQUIRED', 'FIELD_TYPE_INVALID', 'FIELD_TOO_SHORT', 'FIELD_TOO_LONG', 'FIELD_OUT_OF_RANGE', 'FIELD_INVALID_FORMAT', 'FIELD_NOT_ALLOWED', 'FIELD_DUPLICATE', 'FIELD_MIN_ITEMS']);
  });

  it('sends only the role and operator codes to the forbidden page', () => {
    expect([...FORBIDDEN_PAGE_CODES].sort()).toEqual(['INSUFFICIENT_ROLE', 'PLATFORM_OPERATOR_REQUIRED']);
  });

  it('has an English and a Spanish message for every top-level code and issue code', () => {
    for (const bundle of [en, es]) {
      expect(Object.keys(bundle.errors.codes).sort()).toEqual([...Object.values(ApiErrorCode)].sort());

      for (const code of Object.values(IssueCode)) {
        expect(Object.keys(bundle.validation.rules)).toContain(code);
      }
    }
  });
});

describe('apiTraceId', () => {
  const traceId = '0af7651916cd43dd8448eb211c80319c';

  it('reads meta.traceId of the envelope', () => {
    const error = failure(500, { data: null, meta: { traceId, timestamp: 't' }, error: { code: 'INTERNAL_ERROR', message: 'm', issues: [] } });

    expect(apiTraceId(error)).toBe(traceId);
  });

  it('falls back to the X-Trace-Id header when the body is not an envelope', () => {
    const error = new HttpErrorResponse({ status: 502, error: '<html>Bad Gateway</html>', headers: new HttpHeaders({ 'X-Trace-Id': traceId }) });

    expect(apiTraceId(error)).toBe(traceId);
  });

  it('falls back to the header when meta.traceId is empty or not a string', () => {
    const headers = new HttpHeaders({ 'X-Trace-Id': traceId });

    for (const badTraceId of ['', 42, null]) {
      const error = new HttpErrorResponse({ status: 500, error: { data: null, meta: { traceId: badTraceId, timestamp: 't' }, error: null }, headers });

      expect(apiTraceId(error)).toBe(traceId);
    }

    expect(apiTraceId(failure(500, { meta: { traceId: '' } }))).toBeNull();
  });

  it('answers null without an envelope meta or header, and for non-HTTP errors', () => {
    expect(apiTraceId(failure(502, '<html></html>'))).toBeNull();
    expect(apiTraceId(failure(500, null))).toBeNull();
    expect(apiTraceId(failure(500, { error: { code: 'X' } }))).toBeNull();
    expect(apiTraceId(failure(500, { meta: null }))).toBeNull();
    expect(apiTraceId(new Error('plain'))).toBeNull();
  });
});

describe('errorMessage', () => {
  it('translates a registry code that has a key', () => {
    const transloco = configureTranslations();

    expect(errorMessage(transloco, failure(403, envelope('TENANT_SUSPENDED')))).toBe(es.errors.codes.TENANT_SUSPENDED);
  });

  it('falls back to the generic detail for an unknown code', () => {
    const transloco = configureTranslations();

    expect(errorMessage(transloco, failure(409, envelope('SOME_FUTURE_CODE')))).toBe(es['errors']['generic-error'].detail);
  });

  it('answers the operation-failed text for status 0', () => {
    const transloco = configureTranslations();

    expect(errorMessage(transloco, failure(0, null))).toBe(es.errors.operationFailed);
  });

  it('answers the operation-failed text for a 5xx without an envelope', () => {
    const transloco = configureTranslations();

    expect(errorMessage(transloco, failure(500, null))).toBe(es.errors.operationFailed);
  });

  it('answers the generic detail for a client error without an envelope', () => {
    const transloco = configureTranslations();

    expect(errorMessage(transloco, failure(400, null))).toBe(es['errors']['generic-error'].detail);
  });
});

describe('validation messages', () => {
  it('translates the generic rules the server can answer', () => {
    const transloco = configureTranslations();

    for (const code of ['required', 'min', 'max', 'range', 'oneof', 'email', 'datetime']) {
      expect(validationMessage(transloco, code)).not.toBe(validationMessage(transloco, 'unknown_rule'));
      expect(validationMessage(transloco, code)).not.toContain('validation.rules');
    }
  });

  it('falls back to the generic message for a code without its own key', () => {
    const transloco = configureTranslations();

    expect(validationMessage(transloco, 'unknown_rule')).toBe('Valor inválido');
  });
});

describe('SubmitErrors', () => {
  const traceId = '0af7651916cd43dd8448eb211c80319c';

  const issues = [
    { field: 'name', code: 'FIELD_REQUIRED', message: 'a' },
    { field: 'email', code: 'FIELD_INVALID_FORMAT', message: 'b' },
    { field: 'scopes[0]', code: 'FIELD_NOT_ALLOWED', message: 'c' }
  ];

  it('records the status and the code of a 422', () => {
    const errors = new SubmitErrors();

    errors.record(failure(422, envelope('VALIDATION_FAILED', issues, traceId)));

    expect(errors.status()).toBe(422);
    expect(errors.code()).toBe('VALIDATION_FAILED');
  });

  it('records the three issues of a 422', () => {
    const errors = new SubmitErrors();

    errors.record(failure(422, envelope('VALIDATION_FAILED', issues, traceId)));

    expect(errors.issues()).toEqual(issues);
  });

  it('records the trace id of a 422', () => {
    const errors = new SubmitErrors();

    errors.record(failure(422, envelope('VALIDATION_FAILED', issues, traceId)));

    expect(errors.traceId()).toBe(traceId);
  });

  it('clears the recorded trace id and issues', () => {
    const errors = new SubmitErrors();

    errors.record(failure(422, envelope('VALIDATION_FAILED', issues, traceId)));
    errors.clear();

    expect(errors.traceId()).toBeNull();
    expect(errors.issues()).toEqual([]);
  });

  it('leaves no trace id for a non-HTTP failure', () => {
    const errors = new SubmitErrors();

    errors.record(new Error('plain'));

    expect(errors.status()).toBe(0);
    expect(errors.traceId()).toBeNull();
  });

  it('records the status and the code of a failed write', () => {
    const errors = new SubmitErrors();

    errors.record(failure(409, envelope('LAST_TENANT_ADMIN')));

    expect(errors.conflictWith('LAST_TENANT_ADMIN')).toBe(true);
    expect(errors.traceId()).toBe('abc');
    expect(errors.conflictWith('OTHER')).toBe(false);
    expect(errors.serverFailed()).toBe(false);
  });

  it('flags a status 0 or 5xx as a server failure and clears on demand', () => {
    const errors = new SubmitErrors();

    errors.record(failure(0, null));

    expect(errors.serverFailed()).toBe(true);

    errors.clear();

    expect(errors.serverFailed()).toBe(false);
    expect(errors.issues()).toEqual([]);
  });

  it('collects the messages of one field, counting indexed fields as their list', () => {
    const transloco = configureTranslations();

    const errors = new SubmitErrors();

    errors.record(
      failure(
        422,
        envelope('VALIDATION_FAILED', [
          { field: 'scopes[1]', code: 'required', message: 'required' },
          { field: 'name', code: 'max', message: 'too long' }
        ])
      )
    );

    expect(errors.messagesFor(transloco, 'scopes')).toEqual(['Este campo es obligatorio']);
    expect(errors.messagesFor(transloco, 'name')).toEqual(['El valor supera el máximo permitido']);
    expect(errors.messagesFor(transloco, 'description')).toEqual([]);
  });
});
