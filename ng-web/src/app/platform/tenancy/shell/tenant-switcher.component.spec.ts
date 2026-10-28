import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NavigationEnd, Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { accessContext, provideFakeAccess } from '../../../../testing/fixtures/access-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import type { AccessContext, MembershipRef, TenantRef } from '@/platform/core';
import { TenantSwitcher } from './tenant-switcher.component';

function tenant(slug: string, name: string, status: TenantRef['status'] = 'active'): TenantRef {
  return { id: `id-${slug}`, slug, name, status };
}

function membership(slug: string, name: string, status: MembershipRef['status'] = 'active', tenantStatus: TenantRef['status'] = 'active'): MembershipRef {
  return { id: `membership-${slug}`, tenant: tenant(slug, name, tenantStatus), role: 'admin', status };
}

const NAMES: Readonly<Record<string, string>> = { 'tenant-a': 'Tenant A', 'tenant-b': 'Tenant B' };

/** Admin of tenant-a with active access to tenant-b, a suspended tenant and a suspended membership. */
function multiTenantContext(activeSlug = 'tenant-a'): AccessContext {
  return accessContext({
    activeTenant: tenant(activeSlug, NAMES[activeSlug]),
    memberships: [membership('tenant-a', 'Tenant A'), membership('tenant-b', 'Tenant B'), membership('tenant-c', 'Tenant C', 'active', 'suspended'), membership('tenant-d', 'Tenant D', 'suspended')]
  });
}

function refusal(code: string, status = 403): HttpErrorResponse {
  return new HttpErrorResponse({ status, statusText: 'Refused', error: errorEnvelope(code) });
}

/** A switch still in flight, refused later, as a real request is: the select renders the pick before the refusal lands. */
function deferredRefusal(): { readonly promise: Promise<void>; readonly refuse: (error: HttpErrorResponse) => void } {
  let refuse: (error: HttpErrorResponse) => void = () => undefined;

  const promise = new Promise<void>((_resolve, reject) => {
    refuse = reject;
  });

  return { promise, refuse };
}

describe('TenantSwitcher', () => {
  let router: Router;

  let messages: { add: ReturnType<typeof vi.fn> };

  let visitor: ReturnType<typeof signal<AccessContext | null>>;

  let switchTenant: ReturnType<typeof vi.fn<(tenantId: string) => Promise<void>>>;

  beforeEach(() => {
    stubMatchMedia();
    // jsdom does not implement scrollIntoView, which the open select calls.
    Element.prototype.scrollIntoView = vi.fn();
    messages = { add: vi.fn() };
    visitor = signal<AccessContext | null>(null);
    // Like the real provider: a successful switch makes the new tenant active.
    switchTenant = vi.fn((tenantId: string) => {
      visitor.set(multiTenantContext(tenantId.replace('id-', '')));

      return Promise.resolve();
    });
    configureFeatureTest(
      [
        { path: 'work', component: BlankComponent },
        { path: '', component: BlankComponent }
      ],
      [...provideFakeAccess(visitor, { switchTenant: (tenantId) => switchTenant(tenantId) }), { provide: MessageService, useValue: messages }]
    );
    router = TestBed.inject(Router);
  });

  async function renderSwitcher(context: AccessContext | null = multiTenantContext()): Promise<ComponentFixture<TenantSwitcher>> {
    visitor.set(context);
    await router.navigateByUrl('/work');

    const fixture = TestBed.createComponent(TenantSwitcher);

    await settle(fixture);

    return fixture;
  }

  async function openOptions(fixture: ComponentFixture<TenantSwitcher>): Promise<HTMLElement[]> {
    (byTestId(fixture.nativeElement, 'tenant-switcher') as HTMLElement).click();
    await settle(fixture);

    return allByTestId(document.body, 'tenant-option');
  }

  it('lists only active memberships of active tenants and shows the active slug', async () => {
    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    expect(options.map((option) => option.getAttribute('data-key'))).toEqual(['tenant-a', 'tenant-b']);
    expect(options.map((option) => textOf(option))).toEqual(['Tenant A (tenant-a)', 'Tenant B (tenant-b)']);
    expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-a');
  });

  it('renders nothing for an anonymous visitor', async () => {
    const fixture = await renderSwitcher(null);

    expect(byTestId(fixture.nativeElement, 'tenant-switcher')).toBeNull();
  });

  it('switches the tenant, announces it and re-runs the current URL', async () => {
    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    const finishedUrls: string[] = [];

    const subscription = router.events.subscribe((event) => {
      if (event instanceof NavigationEnd) {
        finishedUrls.push(event.urlAfterRedirects);
      }
    });

    options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' })));
    await vi.waitFor(() => expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-b'));
    await vi.waitFor(() => expect(finishedUrls).toEqual(['/', '/work']));
    subscription.unsubscribe();
    expect(switchTenant).toHaveBeenCalledExactlyOnceWith('id-tenant-b');
    expect(router.url).toBe('/work');
  });

  it('does nothing when the active tenant is picked again', async () => {
    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    options.find((option) => option.getAttribute('data-key') === 'tenant-a')?.click();
    await settle(fixture);

    expect(switchTenant).not.toHaveBeenCalled();
  });

  it.each([
    ['TENANT_SUSPENDED', 'Ese tenant está suspendido.'],
    ['TENANT_ACCESS_DENIED', 'No tienes acceso a ese tenant.'],
    ['MEMBERSHIP_REVOKED', 'Tu membresía en ese tenant fue revocada.']
  ])('keeps the old tenant and toasts the %s refusal when the switch is denied', async (code, text) => {
    const inFlight = deferredRefusal();

    switchTenant.mockReturnValueOnce(inFlight.promise);

    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();
    await settle(fixture);
    inFlight.refuse(refusal(code));

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', summary: text })));
    await settle(fixture);
    expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-a');
    expect(textOf(byTestId(fixture.nativeElement, 'tenant-switcher'))).toContain('Tenant A (tenant-a)');
  });

  it('toasts the generic failure text for a refusal without a known code', async () => {
    switchTenant.mockRejectedValueOnce(refusal('SOMETHING_ELSE'));

    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();

    await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' })));
    expect(messages.add).toHaveBeenCalledTimes(1);
  });

  it('keeps the old tenant without a second toast when the switch hits a server failure', async () => {
    const inFlight = deferredRefusal();

    switchTenant.mockReturnValueOnce(inFlight.promise);

    const fixture = await renderSwitcher();

    const options = await openOptions(fixture);

    options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();
    await settle(fixture);
    inFlight.refuse(refusal('UNAVAILABLE', 503));

    await settle(fixture);
    expect(messages.add).not.toHaveBeenCalled();
    expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-a');
  });
});
