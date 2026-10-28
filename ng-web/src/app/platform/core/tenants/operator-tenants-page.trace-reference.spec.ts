import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { OperatorTenantsPage } from './operator-tenants-page.component';

const LIST_URL = '/api/v1/tenants';

const TRACE_ID = '4bf92f3577b34da6a3ce929d0e0e4736';

// A failed call's trace id (meta.traceId / X-Trace-Id) must reach the person, so support can find the server logs.
describe('OperatorTenantsPage trace reference', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [{ path: 'forbidden', component: BlankComponent }],
      [
        { provide: MessageService, useValue: messages },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    );
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function open(): Promise<ComponentFixture<OperatorTenantsPage>> {
    const view = sessionView();

    await loadSession({ ...view, account: { ...(view['account'] as object), isPlatformOperator: true } });

    const fixture = TestBed.createComponent(OperatorTenantsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(okEnvelope({ items: [], page: 1, size: 100, totalItems: 0 }));
    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Crear tenant');
    });

    return fixture;
  }

  it('shows the trace id of a 500 on create next to the failure text', async () => {
    const fixture = await open();

    const root = fixture.nativeElement as HTMLElement;

    typeInto(byTestId(root, 'tenant-slug') as HTMLInputElement, 'acme');
    typeInto(byTestId(root, 'tenant-name') as HTMLInputElement, 'Acme');
    typeInto(byTestId(root, 'tenant-admin-email') as HTMLInputElement, 'owner@example.com');
    await settle(fixture);
    (byTestId(root, 'tenant-submit') as HTMLButtonElement).click();
    (await nextCall('POST', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR', [], TRACE_ID), { status: 500, statusText: 'Error', headers: { 'X-Trace-Id': TRACE_ID } });
    await settle(fixture);

    expect(textOf(byTestId(root, 'tenant-issues'))).toContain(`Referencia: ${TRACE_ID}`);
  });
});
