import { TestBed } from '@angular/core/testing';
import { ProviderScope, TranslocoService, TranslocoTestingModule } from '@jsverse/transloco';
import { firstValueFrom } from 'rxjs';
import es from '../../../../assets/i18n/es.json';
import { fieldIssueMessages, issueMessage, unmatchedIssueMessages } from './issue-message';

const ALIAS = 'probe';

const PROBE_ES = { marker: 'probe', issues: { PASSWORD_REJECTED: 'La contraseña fue rechazada por la política de contraseñas.' } };

/** A module scope that holds one issue text, standing in for any module: core never imports a module. */
const PROBE_SCOPE: ProviderScope = { scope: 'probe', alias: ALIAS, loader: { es: () => Promise.resolve(PROBE_ES) } };

function transloco(): TranslocoService {
  TestBed.configureTestingModule({
    imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })]
  });

  return TestBed.inject(TranslocoService);
}

/** Loads the probe scope the way a page does, through its inline loader. */
async function withProbeScope(service: TranslocoService): Promise<TranslocoService> {
  await firstValueFrom(service.selectTranslate('marker', {}, PROBE_SCOPE));

  return service;
}

describe('issueMessage', () => {
  it('prefers the text of the module scope', async () => {
    const service = await withProbeScope(transloco());

    expect(issueMessage(service, ALIAS, 'PASSWORD_REJECTED')).toBe(PROBE_ES.issues.PASSWORD_REJECTED);
  });

  it('falls back to the base validation rule', async () => {
    const service = await withProbeScope(transloco());

    expect(issueMessage(service, ALIAS, 'FIELD_REQUIRED')).toBe(es.validation.rules.FIELD_REQUIRED);
  });

  it('falls back to the default rule for an unknown code', async () => {
    const service = await withProbeScope(transloco());

    expect(issueMessage(service, ALIAS, 'SOME_FUTURE_CODE')).toBe(es.validation.rules.default);
  });
});

describe('issue lists', () => {
  const issues = [
    { field: 'email', code: 'FIELD_REQUIRED', message: 'a' },
    { field: 'scopes[1]', code: 'FIELD_NOT_ALLOWED', message: 'b' },
    { field: 'other', code: 'BODY_MALFORMED', message: 'c' }
  ];

  it('collects the messages of one field, counting indexed paths as the field', async () => {
    const service = await withProbeScope(transloco());

    expect(fieldIssueMessages(service, ALIAS, issues, 'scopes')).toEqual([es.validation.rules.FIELD_NOT_ALLOWED]);
    expect(fieldIssueMessages(service, ALIAS, issues, 'missing')).toEqual([]);
  });

  it('collects the messages that belong to no known field', async () => {
    const service = await withProbeScope(transloco());

    expect(unmatchedIssueMessages(service, ALIAS, issues, ['email', 'scopes'])).toEqual([es.validation.rules.BODY_MALFORMED]);
  });
});
