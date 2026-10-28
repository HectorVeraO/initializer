import { Location } from '@angular/common';
import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, byTestId, configureFeatureTest, settle, textOf, typeInto } from '@/core/testing/ui-testing';
import es from '../../../../assets/i18n/es.json';
import coreEs from '../i18n/es.json';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { AcceptInvitationPage } from './accept-invitation-page.component';
import { PendingInvitation } from './pending-invitation.service';

const ACCEPT_URL = '/api/v1/invitations/accept';

const ACCEPT_NEW_URL = '/api/v1/invitations/accept-new';

const TOKEN = 'synthetic-token-value';

const TENANT = { id: '55555555-5555-4555-8555-555555555555', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

const MEMBERSHIP = { id: '66666666-6666-4666-8666-666666666666', tenant: TENANT, role: 'member', status: 'active' };

describe('AcceptInvitationPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [
        { path: 'home', component: BlankComponent },
        { path: 'login', component: BlankComponent },
        { path: 'invitations/accept', component: BlankComponent }
      ],
      [
        { provide: MessageService, useValue: messages },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    );
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    history.replaceState(null, '', '/');
    http.verify();
    vi.unstubAllGlobals();
  });

  /** Opens the page at `/invitations/accept` + `hash`, with the session already answered (`null` = anonymous). */
  async function open(hash: string, view: object | null = sessionView(), search = ''): Promise<ComponentFixture<AcceptInvitationPage>> {
    history.replaceState(null, '', `/invitations/accept${search}${hash}`);
    await loadSession(view);

    const fixture = TestBed.createComponent(AcceptInvitationPage);

    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain(coreEs.accept.title);
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<AcceptInvitationPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  async function fillNew(fixture: ComponentFixture<AcceptInvitationPage>, password = 'synthetic-password', confirm = password): Promise<void> {
    typeInto(byTestId(root(fixture), 'accept-new-fullname') as HTMLInputElement, 'Jane Doe');
    typeInto(byTestId(root(fixture), 'accept-new-password') as HTMLInputElement, password);
    typeInto(byTestId(root(fixture), 'accept-new-password-confirm') as HTMLInputElement, confirm);
    await settle(fixture);
    (byTestId(root(fixture), 'accept-new-submit') as HTMLButtonElement).click();
  }

  it('reads the token from the fragment and strips it from the address bar', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    expect(TestBed.inject(Location).path(true)).toBe('/invitations/accept');
    await vi.waitFor(() => expect(TestBed.inject(Router).url).toBe('/invitations/accept'));
    expect(byTestId(root(fixture), 'accept-submit')).not.toBeNull();
  });

  it('ignores a token in the query string', async () => {
    const fixture = await open('', sessionView(), `?token=${TOKEN}`);

    expect(byTestId(root(fixture), 'accept-invalid')).not.toBeNull();
  });

  it('shows the invalid message with a link home when there is no token', async () => {
    const fixture = await open('');

    expect(textOf(byTestId(root(fixture), 'accept-invalid'))).toContain(coreEs.accept.invalid);
    expect(byTestId(root(fixture), 'accept-submit')).toBeNull();
    expect(byTestId(root(fixture), 'accept-new-submit')).toBeNull();
  });

  it('accepts as the signed-in account, then switches to the invited tenant and goes home', async () => {
    const fixture = await open(`#token=${TOKEN}`, sessionView({ account: { id: 'a', email: 'jane@example.com', fullName: null, role: null, isPlatformOperator: false } }));

    expect(textOf(byTestId(root(fixture), 'accept-as-email'))).toBe('jane@example.com');
    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();

    const post = await nextCall('POST', ACCEPT_URL);

    expect(post.request.body).toEqual({ token: TOKEN });
    post.flush(okEnvelope({ membership: MEMBERSHIP }));

    const switchCall = await nextCall('POST', '/api/v1/auth/switch-tenant');

    expect(switchCall.request.body).toEqual({ tenantId: TENANT.id });
    switchCall.flush(okEnvelope(sessionView({ activeTenant: TENANT })));
    await vi.waitFor(() => expect(TestBed.inject(Router).url).toBe('/home'));
    expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
  });

  it('does not report a failed tenant switch as an invalid invitation', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(okEnvelope({ membership: MEMBERSHIP }));
    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('SERVICE_UNAVAILABLE'), { status: 500, statusText: 'Internal Server Error' });
    await vi.waitFor(async () => {
      await settle(fixture);
      expect(byTestId(root(fixture), 'accept-error')).not.toBeNull();
    });

    expect(TestBed.inject(Router).url).not.toBe('/home');
    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(`${es.errors.codes.SERVICE_UNAVAILABLE} Referencia: abc`);
    expect(textOf(root(fixture))).not.toContain(es.errors.codes.INVITATION_INVALID);
  });

  it('retries only the tenant switch after a failed switch, without sending the accepted token again', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(okEnvelope({ membership: MEMBERSHIP }));
    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(errorEnvelope('SERVICE_UNAVAILABLE'), { status: 500, statusText: 'Internal Server Error' });
    await vi.waitFor(async () => {
      await settle(fixture);
      expect(byTestId(root(fixture), 'accept-error')).not.toBeNull();
    });

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: TENANT })));
    await vi.waitFor(() => expect(TestBed.inject(Router).url).toBe('/home'));
    expect(http.match((request) => request.url === ACCEPT_URL)).toHaveLength(0);
  });

  it.each([404, 422])('shows the invalid, expired or used message after a %s on accept', async (status) => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(errorEnvelope(status === 404 ? 'INVITATION_INVALID' : 'VALIDATION_FAILED'), { status, statusText: 'Error' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(es.errors.codes.INVITATION_INVALID);
  });

  it('also treats a 404 with another code as an invalid invitation', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Not Found' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(es.errors.codes.INVITATION_INVALID);
  });

  it('tells the person to sign out and open the link with the invited account on an email mismatch', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(errorEnvelope('INVITATION_EMAIL_MISMATCH'), { status: 403, statusText: 'Forbidden' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(coreEs.accept.emailMismatch);
  });

  it.each(['TENANT_SUSPENDED', 'ACCOUNT_LOCKED_OR_DISABLED'])('shows the text of a 403 %s', async (code) => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(errorEnvelope(code), { status: 403, statusText: 'Forbidden' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(es.errors.codes[code as 'TENANT_SUSPENDED']);
  });

  it('offers a link home after a 409 MEMBERSHIP_ALREADY_ACTIVE', async () => {
    const fixture = await open(`#token=${TOKEN}`);

    (byTestId(root(fixture), 'accept-submit') as HTMLButtonElement).click();
    (await nextCall('POST', ACCEPT_URL)).flush(errorEnvelope('MEMBERSHIP_ALREADY_ACTIVE'), { status: 409, statusText: 'Conflict' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-already-member'))).toContain(coreEs.accept.alreadyMember);
  });

  it('creates the account and sends the person to /login with the email in the router state, never in the URL', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);

    const post = await nextCall('POST', ACCEPT_NEW_URL);

    expect(post.request.body).toEqual({ token: TOKEN, password: 'synthetic-password', fullName: 'Jane Doe' });
    post.flush(okEnvelope({ membership: MEMBERSHIP, account: { id: 'b', email: 'jane@example.com', fullName: 'Jane Doe' } }), { status: 201, statusText: 'Created' });

    const router = TestBed.inject(Router);

    await vi.waitFor(() => expect(router.url).toBe('/login'));
    expect(router.lastSuccessfulNavigation()?.extras.state).toEqual({ email: 'jane@example.com', noticeKey: 'accountCreated' });
    expect(router.url).not.toContain('jane');
  });

  it('does not send a form with a different confirmation', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture, 'synthetic-password', 'another-password');
    await settle(fixture);
    expect(byTestId(root(fixture), 'accept-new-mismatch')).not.toBeNull();
    expect(http.match((request) => request.url === ACCEPT_NEW_URL)).toHaveLength(0);
  });

  it('does not send a form with an empty name', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    typeInto(byTestId(root(fixture), 'accept-new-password') as HTMLInputElement, 'synthetic-password');
    typeInto(byTestId(root(fixture), 'accept-new-password-confirm') as HTMLInputElement, 'synthetic-password');
    await settle(fixture);
    (byTestId(root(fixture), 'accept-new-submit') as HTMLButtonElement).click();
    await settle(fixture);
    expect(http.match((request) => request.url === ACCEPT_NEW_URL)).toHaveLength(0);
    expect(byTestId(root(fixture), 'accept-new-mismatch')).toBeNull();
  });

  it('keeps the typed values and shows 422 issues under their fields', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);
    (await nextCall('POST', ACCEPT_NEW_URL)).flush(
      errorEnvelope('VALIDATION_FAILED', [
        { field: 'password', code: 'PASSWORD_REJECTED', message: 'x' },
        { field: 'fullName', code: 'FIELD_TOO_LONG', message: 'x' }
      ]),
      { status: 422, statusText: 'Unprocessable' }
    );
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-new-password-error'))).toBe(coreEs.issues.PASSWORD_REJECTED);
    expect(textOf(byTestId(root(fixture), 'accept-new-fullname-error'))).toBe(es.validation.rules.FIELD_TOO_LONG);
    expect((byTestId(root(fixture), 'accept-new-fullname') as HTMLInputElement).value).toBe('Jane Doe');
  });

  it('shows the invalid message when the 422 reports the token', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);
    (await nextCall('POST', ACCEPT_NEW_URL)).flush(errorEnvelope('VALIDATION_FAILED', [{ field: 'token', code: 'FIELD_INVALID_FORMAT', message: 'x' }]), { status: 422, statusText: 'Unprocessable' });
    await settle(fixture);

    expect(byTestId(root(fixture), 'accept-invalid')).not.toBeNull();
  });

  it('shows accept-error without a reference after a 404 on accept-new', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);
    (await nextCall('POST', ACCEPT_NEW_URL)).flush(errorEnvelope('INVITATION_INVALID'), { status: 404, statusText: 'Error' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(es.errors.codes.INVITATION_INVALID);
  });

  it('shows accept-error with the trace reference after a 503 on accept-new', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);
    (await nextCall('POST', ACCEPT_NEW_URL)).flush(errorEnvelope('IDENTITY_PROVIDER_UNAVAILABLE'), { status: 503, statusText: 'Error' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(`${es.errors.codes.IDENTITY_PROVIDER_UNAVAILABLE} Referencia: abc`);
  });

  it.each(['IDENTITY_ALREADY_EXISTS', 'MEMBERSHIP_ALREADY_ACTIVE'])('hints to sign in after a 409 %s', async (code) => {
    const fixture = await open(`#token=${TOKEN}`, null);

    await fillNew(fixture);
    (await nextCall('POST', ACCEPT_NEW_URL)).flush(errorEnvelope(code), { status: 409, statusText: 'Conflict' });
    await settle(fixture);

    expect(textOf(byTestId(root(fixture), 'accept-error'))).toBe(coreEs.accept.accountExists);
  });

  it('carries the token through the sign-in round trip', async () => {
    const fixture = await open(`#token=${TOKEN}`, null);

    (byTestId(root(fixture), 'accept-sign-in') as HTMLButtonElement).click();

    const router = TestBed.inject(Router);

    await vi.waitFor(() => expect(router.url).toBe('/login?returnUrl=%2Finvitations%2Faccept'));

    // After the login the page is created again with no fragment and finds the token.
    const returned = TestBed.createComponent(AcceptInvitationPage);

    await settle(returned);
    expect(byTestId(root(returned), 'accept-new-submit')).not.toBeNull();
    expect(byTestId(root(returned), 'accept-invalid')).toBeNull();
  });

  it('forgets the pending token once the page took it', async () => {
    TestBed.inject(PendingInvitation).set(TOKEN);

    const fixture = await open('', null);

    expect(byTestId(root(fixture), 'accept-new-submit')).not.toBeNull();
    expect(TestBed.inject(PendingInvitation).take()).toBeNull();
  });
});
