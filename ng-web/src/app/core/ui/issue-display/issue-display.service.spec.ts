import { HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import es from '../../../../assets/i18n/es.json';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { IssueDisplayService } from './issue-display.service';

const TRACE_ID = '0af7651916cd43dd8448eb211c80319c';

describe('IssueDisplayService.showErrorToast', () => {
  let messages: { add: ReturnType<typeof vi.fn> };

  let service: IssueDisplayService;

  beforeEach(() => {
    messages = { add: vi.fn() };
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [{ provide: MessageService, useValue: messages }]
    });
    service = TestBed.inject(IssueDisplayService);
  });

  it('shows one toast per issue and answers true', () => {
    const issues = [
      { field: 'a', code: 'FIELD_REQUIRED', message: 'one' },
      { field: 'b', code: 'FIELD_TOO_LONG', message: 'two' }
    ];

    const shown = service.showErrorToast(new HttpErrorResponse({ status: 422, error: errorEnvelope('VALIDATION_FAILED', issues, TRACE_ID) }));

    expect(shown).toBe(true);
    expect(messages.add).toHaveBeenCalledTimes(2);
  });

  it('shows the message of the error code and the reference line when there are no issues', () => {
    const shown = service.showErrorToast(new HttpErrorResponse({ status: 403, error: errorEnvelope('TENANT_SUSPENDED', [], TRACE_ID) }));

    expect(shown).toBe(false);
    expect(messages.add).toHaveBeenCalledTimes(1);
    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: `${es.errors.codes.TENANT_SUSPENDED} Referencia: ${TRACE_ID}` }));
  });

  it('takes the reference from the X-Trace-Id header when the body is not an envelope', () => {
    service.showErrorToast(new HttpErrorResponse({ status: 502, error: '<html></html>', headers: new HttpHeaders({ 'X-Trace-Id': TRACE_ID }) }));

    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ detail: `${es.errors.operationFailed} Referencia: ${TRACE_ID}` }));
  });

  it('omits the reference line when no trace id is known', () => {
    service.showErrorToast(new HttpErrorResponse({ status: 400, error: null }));

    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ detail: es['errors']['generic-error'].detail }));
  });
});
