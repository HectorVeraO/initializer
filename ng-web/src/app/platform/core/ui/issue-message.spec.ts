import { TestBed } from '@angular/core/testing';
import { TranslocoService, TranslocoTestingModule } from '@jsverse/transloco';
import { firstValueFrom } from 'rxjs';
import es from '../../../../assets/i18n/es.json';
import tenancyEs from '../i18n/tenancy/es.json';
import { TENANCY_I18N } from '../i18n/tenancy/scope';
import { fieldIssueMessages, issueMessage, unmatchedIssueMessages } from './issue-message';

const ALIAS = TENANCY_I18N.alias as string;

function transloco(): TranslocoService {
  TestBed.configureTestingModule({
    imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })]
  });

  return TestBed.inject(TranslocoService);
}

/** Loads the core scope the way a page does, through its inline loader. */
async function withTenancyScope(service: TranslocoService): Promise<TranslocoService> {
  await firstValueFrom(service.selectTranslate('shell.noTenant', {}, TENANCY_I18N));

  return service;
}

describe('issueMessage', () => {
  it('prefers the text of the module scope', async () => {
    const service = await withTenancyScope(transloco());

    expect(issueMessage(service, ALIAS, 'PASSWORD_REJECTED')).toBe(tenancyEs.issues.PASSWORD_REJECTED);
  });

  it('falls back to the base validation rule', async () => {
    const service = await withTenancyScope(transloco());

    expect(issueMessage(service, ALIAS, 'FIELD_REQUIRED')).toBe(es.validation.rules.FIELD_REQUIRED);
  });

  it('falls back to the default rule for an unknown code', async () => {
    const service = await withTenancyScope(transloco());

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
    const service = await withTenancyScope(transloco());

    expect(fieldIssueMessages(service, ALIAS, issues, 'scopes')).toEqual([es.validation.rules.FIELD_NOT_ALLOWED]);
    expect(fieldIssueMessages(service, ALIAS, issues, 'missing')).toEqual([]);
  });

  it('collects the messages that belong to no known field', async () => {
    const service = await withTenancyScope(transloco());

    expect(unmatchedIssueMessages(service, ALIAS, issues, ['email', 'scopes'])).toEqual([es.validation.rules.BODY_MALFORMED]);
  });
});
