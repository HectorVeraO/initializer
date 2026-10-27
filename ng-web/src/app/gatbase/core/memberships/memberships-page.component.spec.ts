import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { SessionStore } from '../session/session-store.service';
import { MembershipsPage } from './memberships-page.component';

const LIST_URL = '/api/v1/memberships';

const TENANT = { id: '11111111-1111-4111-8111-111111111111', slug: 'tenant-a', name: 'Tenant A', status: 'active' };

function member(name: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { id: `id-${name}`, tenant: TENANT, role: 'member', status: 'active', account: { id: `acc-${name}`, email: `${name}@example.com`, fullName: name }, createdAt: '2026-01-01T12:00:00Z', ...overrides };
}

function pageOf(items: Record<string, unknown>[], totalItems = items.length): object {
  return okEnvelope({ items, page: 1, size: 100, totalItems });
}

describe('MembershipsPage', () => {
  let http: HttpTestingController;

  let messages: { add: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    messages = { add: vi.fn() };
    configureFeatureTest(
      [
        { path: 'memberships', component: BlankComponent },
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

  async function open(items: Record<string, unknown>[] = [], options: { role?: string; totalItems?: number } = {}): Promise<ComponentFixture<MembershipsPage>> {
    const view = sessionView();

    await loadSession({ ...view, account: { ...(view['account'] as object), role: options.role ?? 'admin' } });

    const fixture = TestBed.createComponent(MembershipsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(pageOf(items, options.totalItems ?? items.length));
    await settle(fixture);
    // The core translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Miembros');
    });

    return fixture;
  }

  function root(fixture: ComponentFixture<MembershipsPage>): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function rowOf(fixture: ComponentFixture<MembershipsPage>, name: string): HTMLElement {
    const row = allByTestId(root(fixture), 'memberships-row').find((candidate) => candidate.getAttribute('data-key') === `${name}@example.com`);

    return row as HTMLElement;
  }

  async function pickRole(fixture: ComponentFixture<MembershipsPage>, name: string, label: string): Promise<void> {
    (byTestId(rowOf(fixture, name), 'membership-role-select') as HTMLElement).click();
    await settle(fixture);

    const option = allByTestId(document.body, 'membership-role-option').find((candidate) => textOf(candidate) === label);

    (option as HTMLElement).click();
    await settle(fixture);
  }

  async function confirm(fixture: ComponentFixture<MembershipsPage>, name: string, testId: string): Promise<void> {
    (byTestId(rowOf(fixture, name), testId) as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, name), `${testId}-confirm`) as HTMLButtonElement).click();
  }

  it('lists the members with name, role and status and asks for the first page sorted by email', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(MembershipsPage);

    fixture.detectChanges();

    const request = await nextCall('GET', LIST_URL);

    expect(request.request.params.get('size')).toBe('100');
    expect(request.request.params.get('sort')).toBe('email');
    request.flush(pageOf([member('jane'), member('omar', { role: 'viewer', status: 'suspended' })]));
    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(rowOf(fixture, 'omar'), 'memberships-cell-status'))).toBe('Suspendido');
    });

    expect(rowOf(fixture, 'omar').getAttribute('data-key')).toBe('omar@example.com');
    expect(textOf(byTestId(rowOf(fixture, 'omar'), 'memberships-cell-status'))).toBe('Suspendido');
    expect(byTestId(root(fixture), 'memberships-truncated')).toBeNull();
  });

  it.each(['viewer', 'member'])('shows the list without write controls to a %s', async (role) => {
    const fixture = await open([member('jane')], { role });

    expect(allByTestId(root(fixture), 'memberships-row')).toHaveLength(1);
    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-role'))).toBe('Miembro');
    expect(byTestId(root(fixture), 'membership-role-select')).toBeNull();
    expect(byTestId(root(fixture), 'membership-suspend')).toBeNull();
    expect(byTestId(root(fixture), 'membership-revoke')).toBeNull();
    expect(byTestId(root(fixture), 'membership-reactivate')).toBeNull();
  });

  it('shows suspend on active rows and reactivate on suspended rows to an admin', async () => {
    const fixture = await open([member('jane'), member('omar', { status: 'suspended' })]);

    expect(byTestId(rowOf(fixture, 'jane'), 'membership-suspend')).not.toBeNull();
    expect(byTestId(rowOf(fixture, 'jane'), 'membership-reactivate')).toBeNull();
    expect(byTestId(rowOf(fixture, 'omar'), 'membership-suspend')).toBeNull();
    expect(byTestId(rowOf(fixture, 'omar'), 'membership-reactivate')).not.toBeNull();
    expect(byTestId(rowOf(fixture, 'omar'), 'membership-revoke')).not.toBeNull();
  });

  it('says how many are shown when the list is longer than the page', async () => {
    const fixture = await open([member('jane')], { totalItems: 250 });

    expect(textOf(byTestId(root(fixture), 'memberships-truncated'))).toContain('1 de 250');
  });

  it('shows the empty state for a tenant without members', async () => {
    const fixture = await open([]);

    expect(byTestId(root(fixture), 'memberships-empty')).not.toBeNull();
  });

  it('shows a load error with retry that loads again', async () => {
    await loadSession(sessionView());

    const fixture = TestBed.createComponent(MembershipsPage);

    fixture.detectChanges();
    (await nextCall('GET', LIST_URL)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);
    expect(byTestId(root(fixture), 'memberships-load-error')).not.toBeNull();

    (byTestId(root(fixture), 'memberships-retry') as HTMLButtonElement).click();
    (await nextCall('GET', LIST_URL)).flush(pageOf([member('jane')]));
    await settle(fixture);
    expect(allByTestId(root(fixture), 'memberships-row')).toHaveLength(1);
  });

  it('sends only the role on a role change and shows the saved row', async () => {
    const fixture = await open([member('jane')]);

    await pickRole(fixture, 'jane', 'Lector');

    const patch = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    expect(patch.request.body).toEqual({ role: 'viewer' });
    patch.flush(okEnvelope(member('jane', { role: 'viewer' })));
    await settle(fixture);

    expect(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-role')?.getAttribute('data-value')).toBe('viewer');
    expect(byTestId(root(fixture), 'memberships-row-error')).toBeNull();
  });

  it('sends only the status on suspend and on reactivate', async () => {
    const fixture = await open([member('jane')]);

    await confirm(fixture, 'jane', 'membership-suspend');

    const suspend = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    expect(suspend.request.body).toEqual({ status: 'suspended' });
    suspend.flush(okEnvelope(member('jane', { status: 'suspended' })));
    await settle(fixture);
    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-status'))).toBe('Suspendido');

    (byTestId(rowOf(fixture, 'jane'), 'membership-reactivate') as HTMLButtonElement).click();

    const reactivate = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    expect(reactivate.request.body).toEqual({ status: 'active' });
    reactivate.flush(okEnvelope(member('jane')));
    await settle(fixture);
    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-status'))).toBe('Activo');
  });

  it('sends nothing when the suspend is cancelled', async () => {
    const fixture = await open([member('jane')]);

    (byTestId(rowOf(fixture, 'jane'), 'membership-suspend') as HTMLButtonElement).click();
    await settle(fixture);
    (byTestId(rowOf(fixture, 'jane'), 'membership-suspend-cancel') as HTMLButtonElement).click();
    await settle(fixture);

    expect(byTestId(rowOf(fixture, 'jane'), 'membership-suspend')).not.toBeNull();
  });

  it('removes a revoked row from the list', async () => {
    const fixture = await open([member('jane'), member('omar')]);

    await confirm(fixture, 'jane', 'membership-revoke');

    const patch = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    expect(patch.request.body).toEqual({ status: 'revoked' });
    patch.flush(okEnvelope(member('jane', { status: 'revoked' })));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'memberships-row').map((row) => row.getAttribute('data-key'))).toEqual(['omar@example.com']);
  });

  it('shows the last-admin message on the row and reverts the select on 409 LAST_TENANT_ADMIN', async () => {
    const fixture = await open([member('jane', { role: 'admin' })]);

    await pickRole(fixture, 'jane', 'Lector');
    (await nextCall('PATCH', `${LIST_URL}/id-jane`)).flush(errorEnvelope('LAST_TENANT_ADMIN'), { status: 409, statusText: 'Conflict' });
    await settle(fixture);

    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-row-error'))).toBe('Un tenant necesita al menos un administrador activo.');
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(rowOf(fixture, 'jane'), 'membership-role-select'))).toContain('Administrador');
    });
    expect(messages.add).not.toHaveBeenCalled();
  });

  it('shows the last-admin message when suspending the last admin', async () => {
    const fixture = await open([member('jane', { role: 'admin' })]);

    await confirm(fixture, 'jane', 'membership-suspend');
    (await nextCall('PATCH', `${LIST_URL}/id-jane`)).flush(errorEnvelope('LAST_TENANT_ADMIN'), { status: 409, statusText: 'Conflict' });
    await settle(fixture);

    expect(byTestId(rowOf(fixture, 'jane'), 'memberships-row-error')).not.toBeNull();
    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-status'))).toBe('Activo');
  });

  it('refetches and tells the row is gone after a 404', async () => {
    const fixture = await open([member('jane')]);

    await confirm(fixture, 'jane', 'membership-suspend');
    (await nextCall('PATCH', `${LIST_URL}/id-jane`)).flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Not Found' });
    (await nextCall('GET', LIST_URL)).flush(pageOf([]));
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'warn', summary: expect.stringContaining('ya no existe') })));
    expect(allByTestId(root(fixture), 'memberships-row')).toHaveLength(0);
  });

  it('refetches when the active tenant changes', async () => {
    const fixture = await open([member('jane')]);

    const other = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

    const switching = TestBed.inject(SessionStore).switchTenant(other.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: other })));
    await switching;
    (await nextCall('GET', LIST_URL)).flush(pageOf([member('omar')]));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'memberships-row').map((row) => row.getAttribute('data-key'))).toEqual(['omar@example.com']);
  });

  it('ignores a write answer that arrives after the active tenant changed', async () => {
    const fixture = await open([member('jane')]);

    await confirm(fixture, 'jane', 'membership-suspend');

    const pending = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    const other = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

    const switching = TestBed.inject(SessionStore).switchTenant(other.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: other })));
    await switching;
    (await nextCall('GET', LIST_URL)).flush(pageOf([member('omar')], 250));
    await settle(fixture);
    pending.flush(okEnvelope(member('jane', { status: 'revoked' })));
    await settle(fixture);

    expect(allByTestId(root(fixture), 'memberships-row').map((row) => row.getAttribute('data-key'))).toEqual(['omar@example.com']);
    expect(textOf(byTestId(root(fixture), 'memberships-truncated'))).toContain('1 de 250');
  });

  it('ignores a write failure that arrives after the active tenant changed', async () => {
    const fixture = await open([member('jane')]);

    await confirm(fixture, 'jane', 'membership-suspend');

    const pending = await nextCall('PATCH', `${LIST_URL}/id-jane`);

    const other = { id: '44444444-4444-4444-8444-444444444444', slug: 'tenant-b', name: 'Tenant B', status: 'active' };

    const switching = TestBed.inject(SessionStore).switchTenant(other.id);

    (await nextCall('POST', '/api/v1/auth/switch-tenant')).flush(okEnvelope(sessionView({ activeTenant: other })));
    await switching;
    (await nextCall('GET', LIST_URL)).flush(pageOf([member('omar')]));
    await settle(fixture);
    messages.add.mockClear();
    pending.flush(errorEnvelope('RESOURCE_NOT_FOUND'), { status: 404, statusText: 'Not Found' });
    await settle(fixture);

    expect(messages.add).not.toHaveBeenCalled();
    expect(allByTestId(root(fixture), 'memberships-row').map((row) => row.getAttribute('data-key'))).toEqual(['omar@example.com']);
  });

  it('toasts an error and keeps the row on a 500 when changing the status', async () => {
    const fixture = await open([member('jane')]);

    await confirm(fixture, 'jane', 'membership-suspend');
    (await nextCall('PATCH', `${LIST_URL}/id-jane`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' })));
    expect(textOf(byTestId(rowOf(fixture, 'jane'), 'memberships-cell-status'))).toBe('Activo');
  });

  it('toasts an error and reverts the select on a 500 when changing the role', async () => {
    const fixture = await open([member('jane')]);

    await pickRole(fixture, 'jane', 'Lector');
    (await nextCall('PATCH', `${LIST_URL}/id-jane`)).flush(errorEnvelope('INTERNAL_ERROR'), { status: 500, statusText: 'Error' });
    await settle(fixture);

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' })));
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(byTestId(rowOf(fixture, 'jane'), 'membership-role-select'))).toContain('Miembro');
    });
  });

  it('lists a one-member tenant', async () => {
    const fixture = await open([member('solo', { role: 'admin' })]);

    expect(allByTestId(root(fixture), 'memberships-row')).toHaveLength(1);
  });
});
