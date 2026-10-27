import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { TranslocoService, TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../../assets/i18n/es.json';
import { apiIssues, errorCode, fieldIssueCode, isServerFailure, showsLoadError, validationMessage } from './api-error';
import { SubmitErrors } from './submit-errors';

function failure(status: number, body: unknown): HttpErrorResponse {
  return new HttpErrorResponse({ status, error: body });
}

function envelope(code: string, issues: unknown[] = []): object {
  return { data: null, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: { code, message: 'text', issues } };
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
    const error = failure(403, JSON.stringify(envelope('PERMISSION_DENIED')));

    expect(errorCode(error)).toBe('PERMISSION_DENIED');
  });

  it('tells a server failure (status 0 or 5xx) from a client error', () => {
    expect(isServerFailure(failure(0, null))).toBe(true);
    expect(isServerFailure(failure(503, null))).toBe(true);
    expect(isServerFailure(failure(409, null))).toBe(false);
    expect(isServerFailure(failure(422, null))).toBe(false);
    expect(isServerFailure('nope')).toBe(false);
  });

  it('hides the load error only for a permission-denied answer', () => {
    expect(showsLoadError(failure(403, envelope('PERMISSION_DENIED')))).toBe(false);
    expect(showsLoadError(failure(403, envelope('OTHER')))).toBe(true);
    expect(showsLoadError(failure(503, null))).toBe(true);
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
  it('records the status and the code of a failed write', () => {
    const errors = new SubmitErrors();

    errors.record(failure(409, envelope('LAST_TENANT_ADMIN')));

    expect(errors.conflictWith('LAST_TENANT_ADMIN')).toBe(true);
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
