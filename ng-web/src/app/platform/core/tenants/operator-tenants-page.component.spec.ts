import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import { errorEnvelope, okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession } from '../../../../testing/fixtures/session-testing';
import { nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { OperatorTenantsPage } from './operator-tenants-page.component';

const LIST_URL = '/api/v1/tenants';

const SYNTHETIC_LINK = 'http://localhost/invitations/accept#token=synthetic-token-value';

function tenant(slug: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { id: `id-${slug}`, slug, name: `Name ${slug}`, status: 'active', createdAt: '2026-01-01T12:00:00Z', ...overrides };
}

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('OperatorTenantsPage', () => {
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

  async function open(items: Record<string, unknown>[] = [], totalItems = items.length): Promise<ComponentFixture<OperatorTenantsPage>> {
    const view = sessionView();

    await loadSession({ ...view, account: { ...(view['account'] as object), isPlatformOperator: true } });

    const fixture = TestBed.createComponent(OperatorTenantsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, totalItems));
    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Crear tenant');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<OperatorTenantsPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function rowOf(fixture: ComponentFixture<OperatorTenantsPage>, slug: string): HTMLElement {
    const row = allByTestId(root(fixture), 'tenants-row').find((candidate) => candidate.getAttribute('data-key') === slug);

    return row as HTMLElement;
  }

  async function create(fixture: ComponentFixture<OperatorTenantsPage>, slug = 'acme', name = 'Acme', adminEmail = 'owner@example.com'): Promise<void> {
    typeInto(byTestId(root(fixture), 'tenant-slug') as HTMLInputElement, slug);
    typeInto(byTestId(root(fixture), 'tenant-name') as HTMLInputElement, name);
    typeInto(byTestId(root(fixture), 'tenant-admin-email') as HTMLInputElement, adminEmail);
    await settle(fixture);
    (byTestId(root(fixture), 'tenant-submit') as HTMLButtonElement).click();
  }

  async function failCreate(fixture: ComponentFixture<OperatorTenantsPage>, status: number, body: object): Promise<void> {
    await create(fixture);
    (await nextCall('POST', LIST_URL)).flush(body, { status, statusText: 'Error' });
    await settle(fixture);
  }

  it('lists tenants newest first with status and the suspension note', async () => {
    const fixture = await open([tenant('acme'), tenant('beta', { status: 'suspended' })]);

    expect(allByTestId(root(fixture), 'tenants-row').map((row) => row.getAttribute('data-key'))).toEqual(['acme', 'beta']);
    expect(textOf(byTestId(rowOf(fixture, 'beta'), 'tenants-cell-status'))).toBe('Suspendido');
    expect(textOf(byTestId(root(fixture), 'tenants-suspension-note'))).toContain('API keys');
  });

  it('asks for the first page at the contract maximum, newest first', async () => {
    await loadSession(sessionView({ account: { ...(sessionView()['account'] as object), isPlatformOperator: true } }));

    const fixture = TestBed.createComponent(OperatorTenantsPage);

    fixture.detectChanges();

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.get('sort')).toBe('-createdAt');
    request.flush(pageOf([]));
  });

  it('shows the empty state when there are no tenants', async () => {
    const fixture = await open([]);

    expect(byTestId(root(fixture), 'tenants-empty')).not.toBeNull();
  });

  it('says how many are shown when the list is longer than the page', async () => {
    const fixture = await open([tenant('acme')], 250);

    expect(textOf(byTestId(root(fixture), 'tenants-truncated'))).toContain('1 de 250');
  });

  it('shows a load error with retry that loads again', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(OperatorTenantsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);
    expect(byTestId(root(fixture), 'tenants-load-error')).not.toBeNull();

    (byTestId(root(fixture), 'tenants-retry') as HTMLButtonElement).click();
    (await nextCall('GET', LIST_URL)).flush(pageOf([tenant('acme')]));
    await settle(fixture);
    expect(allByTestId(root(fixture), 'tenants-row')).toHaveLength(1);
  });

  it('creates a tenant, shows the invitation link once in the dialog and never in a row', async () => {
    const fixture = await open([]);

    await create(fixture, ' acme ', ' Acme ', ' owner@example.com ');

    const post = await nextCall('POST', LIST_URL);

    expect(post.request.body).toEqual({ slug: 'acme', name: 'Acme', adminEmail: 'owner@example.com' });
    post.flush(okEnvelope({ tenant: tenant('acme'), invitation: { id: 'inv', email: 'owner@example.com' }, invitationUrl: SYNTHETIC_LINK }), { status: 201, statusText: 'Created' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([tenant('acme')]));
    await settle(fixture);

    const value = byTestId(document.body, 'one-time-secret-value') as HTMLInputElement;

    expect(value.value).toBe(SYNTHETIC_LINK);
    expect(textOf(rowOf(fixture, 'acme'))).not.toContain('synthetic-token-value');
    expect(root(fixture).innerHTML).not.toContain('synthetic-token-value');

    (byTestId(document.body, 'one-time-secret-close') as HTMLButtonElement).click();
    await settle(fixture);

    expect(document.body.innerHTML).not.toContain('synthetic-token-value');
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('puts a 409 TENANT_SLUG_TAKEN under the slug field and keeps what was typed', async () => {
    const fixture = await open([]);

    await failCreate(fixture, 409, errorEnvelope('TENANT_SLUG_TAKEN'));

    expect(textOf(byTestId(root(fixture), 'tenant-slug-error'))).toBe(es.errors.codes.TENANT_SLUG_TAKEN);
    expect(byTestId(root(fixture), 'tenant-issues')).toBeNull();
    expect((byTestId(root(fixture), 'tenant-name') as HTMLInputElement).value).toBe('Acme');
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('shows every 422 issue at once under its field or in the form list', async () => {
    const fixture = await open([]);

    await failCreate(
      fixture,
      422,
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'slug', code: 'FIELD_INVALID_FORMAT', message: 'x' },
        { field: 'name', code: 'FIELD_TOO_LONG', message: 'x' },
        { field: 'adminEmail', code: 'FIELD_INVALID_FORMAT', message: 'x' },
        { field: 'body', code: 'BODY_MALFORMED', message: 'x' }
      ])
    );

    expect(textOf(byTestId(root(fixture), 'tenant-slug-error'))).toBe(es.validation.rules.FIELD_INVALID_FORMAT);
    expect(textOf(byTestId(root(fixture), 'tenant-name-error'))).toBe(es.validation.rules.FIELD_TOO_LONG);
    expect(textOf(byTestId(root(fixture), 'tenant-admin-email-error'))).toBe(es.validation.rules.FIELD_INVALID_FORMAT);
    expect(textOf(byTestId(root(fixture), 'tenant-issues'))).toBe(es.validation.rules.BODY_MALFORMED);
  });

  it('offers suspend on active rows and activate on suspended rows', async () => {
    const fixture = await open([tenant('acme'), tenant('beta', { status: 'suspended' })]);

    expect(byTestId(rowOf(fixture, 'acme'), 'tenant-suspend')).not.toBeNull();
    expect(byTestId(rowOf(fixture, 'acme'), 'tenant-activate')).toBeNull();
    expect(byTestId(rowOf(fixture, 'beta'), 'tenant-suspend')).toBeNull();
    expect(byTestId(rowOf(fixture, 'beta'), 'tenant-activate')).not.toBeNull();
  });

  it('suspends after the inline confirmation and activates again', async () => {
    const fixture = await open([tenant('acme')]);

    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend-confirm') as HTMLButtonElement).click();

    const suspend = await nextCall('PATCH', `${LIST_URL}/id-acme/status`);

    expect(suspend.request.body).toEqual({ status: 'suspended' });
    suspend.flush(okEnvelope(tenant('acme', { status: 'suspended' })));
    await settle(fixture);
    expect(textOf(byTestId(rowOf(fixture, 'acme'), 'tenants-cell-status'))).toBe('Suspendido');

    (byTestId(rowOf(fixture, 'acme'), 'tenant-activate') as HTMLButtonElement).click();

    const activate = await nextCall('PATCH', `${LIST_URL}/id-acme/status`);

    expect(activate.request.body).toEqual({ status: 'active' });
    activate.flush(okEnvelope(tenant('acme')));
    await settle(fixture);
    expect(textOf(byTestId(rowOf(fixture, 'acme'), 'tenants-cell-status'))).toBe('Activo');
  });

  it('sends nothing when the suspend is cancelled', async () => {
    const fixture = await open([tenant('acme')]);

    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend-cancel') as HTMLButtonElement).click();
    await settle(fixture);

    expect(byTestId(rowOf(fixture, 'acme'), 'tenant-suspend')).not.toBeNull();
  });

  it('refetches and tells the row changed after a 404 on suspend', async () => {
    const fixture = await open([tenant('acme')]);

    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend-confirm') as HTMLButtonElement).click();
    (await nextCall('PATCH', `${LIST_URL}/id-acme/status`)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Not Found' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: expect.stringContaining('ya no existe') })));
    expect(allByTestId(root(fixture), 'tenants-row')).toHaveLength(0);
  });

  it('refetches and tells the row changed after a 409 on suspend', async () => {
    const fixture = await open([tenant('acme')]);

    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend-confirm') as HTMLButtonElement).click();
    (await nextCall('PATCH', `${LIST_URL}/id-acme/status`)).flush(errorEnvelope('RESOURCE_CONFLICT'), { status: 409, statusText: 'Conflict' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([tenant('acme', { status: 'suspended' })]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: expect.stringContaining('ya no existe') })));
    expect(textOf(byTestId(rowOf(fixture, 'acme'), 'tenants-cell-status'))).toBe('Suspendido');
  });

  it('warns with the generic message and refetches on a 500 on suspend', async () => {
    const fixture = await open([tenant('acme')]);

    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'acme'), 'tenant-suspend-confirm') as HTMLButtonElement).click();
    (await nextCall('PATCH', `${LIST_URL}/id-acme/status`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([tenant('acme')]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn' })));
    expect(messages.add).not.toHaveBeenCalledWith(expect.objectContaining({ summary: expect.stringContaining('ya no existe') }));
  });

  it('shows the failure in the form list on a 500 on create and keeps what was typed', async () => {
    const fixture = await open([]);

    await failCreate(fixture, 500, errorEnvelope('INTERNAL_ERROR'));

    expect(textOf(byTestId(root(fixture), 'tenant-issues'))).toBe(`${es.errors.codes.INTERNAL_ERROR} Referencia: abc`);
    expect((byTestId(root(fixture), 'tenant-name') as HTMLInputElement).value).toBe('Acme');
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });
});
