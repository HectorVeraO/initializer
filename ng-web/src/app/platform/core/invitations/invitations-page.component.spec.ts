import { formatSantiago } from '@/core/format/santiago-date';
import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { SessionStore } from '../session/session-store.service';
import { InvitationsPage } from './invitations-page.component';

const LIST_URL = '/api/v1/invitations';

const SYNTHETIC_LINK = 'http://localhost/invitations/accept#token=synthetic-token-value';

function invitation(email: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { id: `id-${email.split('@')[0]}`, email, role: 'member', status: 'pending', expiresAt: '2026-02-01T12:00:00Z', createdAt: '2026-01-01T12:00:00Z', acceptedAt: null, revokedAt: null, ...overrides };
}

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('InvitationsPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [
        { path: 'invitations', component: BlankComponent },
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

  async function open(items: Record<string, unknown>[] = [], totalItems = items.length): Promise<ComponentFixture<InvitationsPage>> {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(InvitationsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, totalItems));
    await settle(fixture);
    // The core translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Invitar');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<InvitationsPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  async function invite(fixture: ComponentFixture<InvitationsPage>, email: string): Promise<void> {
    typeInto(byTestId(root(fixture), 'invite-email') as HTMLInputElement, email);
    await settle(fixture);
    (byTestId(root(fixture), 'invite-submit') as HTMLButtonElement).click();
  }

  async function failInvite(fixture: ComponentFixture<InvitationsPage>, status: number, body: object): Promise<void> {
    await invite(fixture, 'jane@example.com');
    (await nextCall('POST', LIST_URL)).flush(body, { status, statusText: 'Error' });
    await settle(fixture);
  }

  it('lists the pending invitations of the page with their role, status and expiry', async () => {
    const fixture = await open([invitation('jane@example.com'), invitation('omar@example.com', { role: 'viewer' })]);

    const rows = allByTestId(root(fixture), 'invitations-row');

    expect(rows.map((row) => row.getAttribute('data-key'))).toEqual(['jane@example.com', 'omar@example.com']);
    expect(textOf(byTestId(rows[0], 'invitations-cell-status'))).toBe('Pendiente');
    expect(textOf(byTestId(rows[1], 'invitations-cell-role'))).toBe('Lector');
    expect(textOf(byTestId(rows[0], 'invitations-cell-expires'))).toBe(formatSantiago('2026-02-01T12:00:00Z'));
    expect(byTestId(root(fixture), 'invitations-truncated')).toBeNull();
  });

  it('asks for the first page at the contract maximum, pending only, newest first', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(InvitationsPage);

    fixture.detectChanges();

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('status')).toBe('pending');
    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.get('sort')).toBe('-createdAt');
    request.flush(pageOf([]));
  });

  it('says how many are shown when the list is longer than the page', async () => {
    const fixture = await open([invitation('jane@example.com')], 250);

    expect(textOf(byTestId(root(fixture), 'invitations-truncated'))).toContain('1 de 250');
  });

  it('shows the empty state when nothing is pending', async () => {
    const fixture = await open([]);

    expect(byTestId(root(fixture), 'invitations-empty')).not.toBeNull();
  });

  it('shows a load error with retry that loads again', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(InvitationsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);
    expect(byTestId(root(fixture), 'invitations-load-error')).not.toBeNull();

    (byTestId(root(fixture), 'invitations-retry') as HTMLButtonElement).click();
    (await nextCall('GET', LIST_URL)).flush(pageOf([invitation('jane@example.com')]));
    await settle(fixture);
    expect(allByTestId(root(fixture), 'invitations-row')).toHaveLength(1);
  });

  it('sends the typed email with the default role and shows the link once, in the dialog only', async () => {
    const fixture = await open([]);

    await invite(fixture, ' jane@example.com ');

    const post = await nextCall('POST', LIST_URL);

    expect(post.request.body).toEqual({ email: 'jane@example.com', role: 'member' });
    post.flush(okEnvelope({ invitation: invitation('jane@example.com'), invitationUrl: SYNTHETIC_LINK }), { status: 201, statusText: 'Created' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([invitation('jane@example.com')]));
    await settle(fixture);

    const value = byTestId(document.body, 'one-time-secret-value') as HTMLInputElement;

    expect(value.value).toBe(SYNTHETIC_LINK);
    expect(textOf(byTestId(root(fixture), 'invitations-row'))).not.toContain('synthetic-token-value');
    expect(root(fixture).innerHTML).not.toContain('synthetic-token-value');

    (byTestId(document.body, 'one-time-secret-close') as HTMLButtonElement).click();
    await settle(fixture);

    expect(document.body.innerHTML).not.toContain('synthetic-token-value');
    expect(document.body.innerHTML).not.toContain('#token=');
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('keeps what was typed and shows every 422 issue under its field or in the form list', async () => {
    const fixture = await open([]);

    await failInvite(
      fixture,
      422,
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'email', code: 'FIELD_INVALID_FORMAT', message: 'x' },
        { field: 'role', code: 'FIELD_NOT_ALLOWED', message: 'x' },
        { field: 'body', code: 'BODY_MALFORMED', message: 'x' }
      ])
    );

    expect(textOf(byTestId(root(fixture), 'invite-email-error'))).toBe(es.validation.rules.FIELD_INVALID_FORMAT);
    expect(textOf(byTestId(root(fixture), 'invite-role-error'))).toBe(es.validation.rules.FIELD_NOT_ALLOWED);
    expect(textOf(byTestId(root(fixture), 'invite-issues'))).toBe(es.validation.rules.BODY_MALFORMED);
    expect((byTestId(root(fixture), 'invite-email') as HTMLInputElement).value).toBe('jane@example.com');
    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('puts a 409 MEMBERSHIP_ALREADY_ACTIVE under the email field', async () => {
    const fixture = await open([]);

    await failInvite(fixture, 409, errorEnvelope('MEMBERSHIP_ALREADY_ACTIVE'));

    expect(textOf(byTestId(root(fixture), 'invite-email-error'))).toBe(es.errors.codes.MEMBERSHIP_ALREADY_ACTIVE);
    expect(byTestId(root(fixture), 'invite-issues')).toBeNull();
  });

  it('puts a 403 PLATFORM_OPERATOR_REQUIRED under the email field and stays on the page', async () => {
    const fixture = await open([]);

    const router = TestBed.inject(Router);

    await router.navigateByUrl('/invitations');
    await failInvite(fixture, 403, errorEnvelope('PLATFORM_OPERATOR_REQUIRED'));

    expect(textOf(byTestId(root(fixture), 'invite-email-error'))).toBe(es.errors.codes.PLATFORM_OPERATOR_REQUIRED);
    expect(router.url).toBe('/invitations');
  });

  it('revokes after the inline confirmation and refetches', async () => {
    const fixture = await open([invitation('jane@example.com')]);

    (byTestId(root(fixture), 'invitation-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'invitation-revoke-confirm') as HTMLButtonElement).click();

    (await nextCall('DELETE', `${LIST_URL}/id-jane`)).flush(okEnvelope(invitation('jane@example.com', { status: 'revoked' })));
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'invitations-row')).toHaveLength(0);
  });

  it('sends nothing when the revoke is cancelled', async () => {
    const fixture = await open([invitation('jane@example.com')]);

    (byTestId(root(fixture), 'invitation-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'invitation-revoke-cancel') as HTMLButtonElement).click();
    await settle(fixture);

    expect(byTestId(root(fixture), 'invitation-revoke')).not.toBeNull();
  });

  it.each([404, 409])('refetches and tells the row is gone after a %s on revoke', async (status) => {
    const fixture = await open([invitation('jane@example.com')]);

    (byTestId(root(fixture), 'invitation-revoke') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(root(fixture), 'invitation-revoke-confirm') as HTMLButtonElement).click();

    (await nextCall('DELETE', `${LIST_URL}/id-jane`)).flush(errorEnvelope(status === 404 ? 'RESOURCE_NOT_FOUND' : 'INVITATION_NOT_PENDING'), { status, statusText: 'Error' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: expect.stringContaining('ya no existe') })));
    expect(allByTestId(root(fixture), 'invitations-row')).toHaveLength(0);
  });

  it('refetches when the active tenant changes', async () => {
    const fixture = await open([invitation('jane@example.com')]);

    const other = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

    const switching = TestBed.inject(SessionStore).switchTenant(other.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: other })));
    await switching;
    (await nextCall('GET', LIST_URL)).flush(pageOf([invitation('omar@example.com')]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'invitations-row').map((row) => row.getAttribute('data-key'))).toEqual(['omar@example.com']);
  });
});
