import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { SessionStore } from '../session/session-store.service';
import { ApiKeysPage, endOfDayUtc } from './api-keys-page.component';

const LIST_URL = '/api/v1/api-keys';

const SCOPES_URL = '/api/v1/api-key-scopes';

const SYNTHETIC_KEY = 'synthetic-plain-key';

const SCOPES = [
  { name: 'api-keys:read', description: 'Read API keys' },
  { name: 'tenant:read', description: 'Read the tenant' }
];

function apiKey(name: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { id: `id-${name}`, name, keyPrefix: 'synthetic_pfx', scopes: ['tenant:read'], status: 'active', expiresAt: null, revokedAt: null, createdAt: '2026-01-01T12:00:00Z', ...overrides };
}

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('endOfDayUtc', () => {
  it('formats the chosen calendar day as 23:59:59 UTC', () => {
    expect(endOfDayUtc(new Date(2030, 0, 5))).toBe('2030-01-05T23:59:59Z');
    expect(endOfDayUtc(new Date(2030, 11, 31))).toBe('2030-12-31T23:59:59Z');
  });
});

describe('ApiKeysPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [
        { path: 'api-keys', component: BlankComponent },
        { path: 'forbidden', component: BlankComponent }
      ],
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

  async function open(items: Record<string, unknown>[] = [], totalItems = items.length, scopes: object[] = SCOPES): Promise<ComponentFixture<ApiKeysPage>> {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(ApiKeysPage);

    fixture.detectChanges();
    (await nextCall('GET', SCOPES_URL)).flush(okEnvelope(scopes));
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, totalItems));
    await settle(fixture);
    // The core translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Crear clave de API');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<ApiKeysPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function scopeBox(fixture: ComponentFixture<ApiKeysPage>, name: string): HTMLInputElement {
    const box = allByTestId(root(fixture), 'api-key-scope').find((element) => element.getAttribute('data-key') === name);

    return box as HTMLInputElement;
  }

  async function fill(fixture: ComponentFixture<ApiKeysPage>, name: string, scopes: string[]): Promise<void> {
    typeInto(byTestId(root(fixture), 'api-key-name') as HTMLInputElement, name);
    for (const scope of scopes) {
      scopeBox(fixture, scope).click();
    }
    await settle(fixture);
  }

  async function submit(fixture: ComponentFixture<ApiKeysPage>): Promise<void> {
    (byTestId(root(fixture), 'api-key-submit') as HTMLButtonElement).click();
    await settle(fixture);
  }

  it('lists the keys with prefix, scopes and status, never a secret', async () => {
    const fixture = await open([apiKey('ci'), apiKey('old', { status: 'revoked' })]);

    const rows = allByTestId(root(fixture), 'api-keys-row');

    expect(rows.map((row) => row.getAttribute('data-key'))).toEqual(['ci', 'old']);
    expect(textOf(byTestId(rows[0], 'api-keys-cell-prefix'))).toBe('synthetic_pfx');
    expect(textOf(byTestId(rows[0], 'api-keys-cell-scopes'))).toBe('tenant:read');
    expect(textOf(byTestId(rows[0], 'api-keys-cell-status'))).toBe('Activa');
    expect(textOf(byTestId(rows[1], 'api-keys-cell-status'))).toBe('Revocada');
    expect(byTestId(rows[0], 'api-key-revoke')).not.toBeNull();
    expect(byTestId(rows[1], 'api-key-revoke')).toBeNull();
    expect(byTestId(root(fixture), 'api-keys-truncated')).toBeNull();
  });

  it('asks for the first page at the contract maximum, newest first', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(ApiKeysPage);

    fixture.detectChanges();
    (await nextCall('GET', SCOPES_URL)).flush(okEnvelope(SCOPES));

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('page')).toBe('1');
    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.get('sort')).toBe('-createdAt');
    request.flush(pageOf([]));
  });

  it('offers the scopes the server returns', async () => {
    const fixture = await open([], 0, [{ name: 'custom:scope', description: 'Synthetic' }]);

    const boxes = allByTestId(root(fixture), 'api-key-scope');

    expect(boxes.map((box) => box.getAttribute('data-key'))).toEqual(['custom:scope']);
  });

  it('says how many are shown when the list is longer than the page', async () => {
    const fixture = await open([apiKey('ci')], 250);

    expect(textOf(byTestId(root(fixture), 'api-keys-truncated'))).toContain('1 de 250');
  });

  it('shows the empty state when there are no keys', async () => {
    const fixture = await open([]);

    expect(byTestId(root(fixture), 'api-keys-empty')).not.toBeNull();
  });

  it('shows an empty scope state and disables submit when no scope is offered', async () => {
    const fixture = await open([], 0, []);

    expect(byTestId(root(fixture), 'api-key-scopes-empty')).not.toBeNull();
    expect((byTestId(root(fixture), 'api-key-submit') as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows a load error with retry that loads again', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(ApiKeysPage);

    fixture.detectChanges();
    (await nextCall('GET', SCOPES_URL)).flush(okEnvelope(SCOPES));
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);
    expect(byTestId(root(fixture), 'api-keys-load-error')).not.toBeNull();

    (byTestId(root(fixture), 'api-keys-retry') as HTMLButtonElement).click();
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('ci')]));
    await settle(fixture);
    expect(allByTestId(root(fixture), 'api-keys-row')).toHaveLength(1);
  });

  async function createKey(fixture: ComponentFixture<ApiKeysPage>): Promise<void> {
    await fill(fixture, 'ci', ['tenant:read']);
    await submit(fixture);

    const post = await nextCall('POST', LIST_URL);

    post.flush(okEnvelope({ apiKey: apiKey('ci'), plainTextKey: SYNTHETIC_KEY }), { status: 201, statusText: 'Created' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('ci')]));
    await settle(fixture);
  }

  it('sends the name, the chosen scopes and a null expiry', async () => {
    const fixture = await open([]);

    await fill(fixture, 'ci', ['tenant:read']);
    await submit(fixture);

    const post = await nextCall('POST', LIST_URL);

    expect(post.request.body).toEqual({ name: 'ci', scopes: ['tenant:read'], expiresAt: null });
    post.flush(okEnvelope({ apiKey: apiKey('ci'), plainTextKey: SYNTHETIC_KEY }), { status: 201, statusText: 'Created' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('ci')]));
    await settle(fixture);
  });

  it('shows the created key in the dialog only, never in the page', async () => {
    const fixture = await open([]);

    await createKey(fixture);

    const value = byTestId(document.body, 'one-time-secret-value') as HTMLInputElement;

    expect(value.value).toBe(SYNTHETIC_KEY);
    expect(textOf(byTestId(root(fixture), 'api-keys-row'))).not.toContain(SYNTHETIC_KEY);
    expect(root(fixture).innerHTML).not.toContain(SYNTHETIC_KEY);
  });

  it('drops the key and resets the form when the dialog closes', async () => {
    const fixture = await open([]);

    await createKey(fixture);
    (byTestId(document.body, 'one-time-secret-close') as HTMLButtonElement).click();
    await settle(fixture);

    expect(document.body.innerHTML).not.toContain(SYNTHETIC_KEY);
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
    expect((byTestId(root(fixture), 'api-key-name') as HTMLInputElement).value).toBe('');
  });

  it('sends a 64-character name untouched', async () => {
    const fixture = await open([]);

    const name = 'n'.repeat(64);

    await fill(fixture, name, ['api-keys:read', 'tenant:read']);
    await submit(fixture);

    const post = await nextCall('POST', LIST_URL);

    expect(post.request.body).toEqual({ name, scopes: ['api-keys:read', 'tenant:read'], expiresAt: null });
    post.flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'name', code: 'FIELD_TOO_LONG', message: 'x' }]), { status: 422, statusText: 'Error' });
    await settle(fixture);
  });

  it('sends the typed expiry day as the end of that day in UTC', async () => {
    const fixture = await open([]);

    await fill(fixture, 'ci', ['tenant:read']);
    const dateInput = root(fixture).querySelector('#api-key-expires-at') as HTMLInputElement;

    // The date picker parses typed text only after a key press.
    dateInput.dispatchEvent(new KeyboardEvent('keydown', { key: '1', bubbles: true }));
    typeInto(dateInput, '2030-01-15');
    await settle(fixture);
    await submit(fixture);

    const post = await nextCall('POST', LIST_URL);

    expect((post.request.body as { expiresAt: string }).expiresAt).toBe('2030-01-15T23:59:59Z');
    post.flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'name', code: 'FIELD_REQUIRED', message: 'x' }]), { status: 422, statusText: 'Error' });
    await settle(fixture);
  });

  it('shows every 422 issue under its field at once and keeps what was typed', async () => {
    const fixture = await open([]);

    typeInto(byTestId(root(fixture), 'api-key-name') as HTMLInputElement, '');
    await submit(fixture);

    (await nextCall('POST', LIST_URL)).flush(
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'name', code: 'FIELD_REQUIRED', message: 'x' },
        { field: 'scopes', code: 'FIELD_REQUIRED', message: 'x' },
        { field: 'scopes[1]', code: 'FIELD_NOT_ALLOWED', message: 'x' },
        { field: 'body', code: 'BODY_MALFORMED', message: 'x' }
      ]),
      { status: 422, statusText: 'Error' }
    );
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'api-key-name-error'))).toBe(es.validation.rules.FIELD_REQUIRED);
    expect(allByTestId(root(fixture), 'api-key-scopes-error').map(textOf)).toEqual([es.validation.rules.FIELD_REQUIRED, es.validation.rules.FIELD_NOT_ALLOWED]);
    expect(textOf(byTestId(root(fixture), 'api-key-issues'))).toBe(es.validation.rules.BODY_MALFORMED);
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('shows a generic form message and no secret after a 500 on create', async () => {
    const fixture = await open([]);

    await fill(fixture, 'ci', ['tenant:read']);
    await submit(fixture);

    (await nextCall('POST', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'api-key-issues'))).toBe(es.errors.codes.INTERNAL_ERROR);
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('revokes after the inline confirmation and shows the row as revoked', async () => {
    const fixture = await open([apiKey('ci')]);

    (byTestId(root(fixture), 'api-key-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'api-key-revoke-confirm') as HTMLButtonElement).click();

    (await nextCall('DELETE', `${LIST_URL}/id-ci`)).flush(okEnvelope(apiKey('ci', { status: 'revoked' })));
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('ci', { status: 'revoked' })]));
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'api-keys-cell-status'))).toBe('Revocada');
    expect(byTestId(root(fixture), 'api-key-revoke')).toBeNull();
  });

  it('sends nothing when the revoke is cancelled', async () => {
    const fixture = await open([apiKey('ci')]);

    (byTestId(root(fixture), 'api-key-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'api-key-revoke-cancel') as HTMLButtonElement).click();
    await settle(fixture);

    expect(byTestId(root(fixture), 'api-key-revoke')).not.toBeNull();
  });

  it('refetches and tells the row is gone after a 404 on revoke', async () => {
    const fixture = await open([apiKey('ci')]);

    (byTestId(root(fixture), 'api-key-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'api-key-revoke-confirm') as HTMLButtonElement).click();

    (await nextCall('DELETE', `${LIST_URL}/id-ci`)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Error' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: expect.stringContaining('ya no existe') })));
    expect(allByTestId(root(fixture), 'api-keys-row')).toHaveLength(0);
  });

  it('refetches and shows the generic warning after a 500 on revoke', async () => {
    const fixture = await open([apiKey('ci')]);

    (byTestId(root(fixture), 'api-key-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'api-key-revoke-confirm') as HTMLButtonElement).click();

    (await nextCall('DELETE', `${LIST_URL}/id-ci`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('ci')]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: es.errors.codes.INTERNAL_ERROR })));
    expect(allByTestId(root(fixture), 'api-keys-row')).toHaveLength(1);
  });

  it('refetches when the active tenant changes', async () => {
    const fixture = await open([apiKey('ci')]);

    const other = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

    const switching = TestBed.inject(SessionStore).switchTenant(other.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: other })));
    await switching;
    (await nextCall('GET', SCOPES_URL)).flush(okEnvelope(SCOPES));
    (await nextCall('GET', LIST_URL)).flush(pageOf([apiKey('other')]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'api-keys-row').map((row) => row.getAttribute('data-key'))).toEqual(['other']);
  });
});
