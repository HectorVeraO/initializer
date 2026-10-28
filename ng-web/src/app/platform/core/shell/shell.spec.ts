import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { NavigationEnd, Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, allByTestId, byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { SESSION_URL, fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { AccountMenu } from './account-menu.component';
import { TenantSwitcher } from './tenant-switcher.component';

const SWITCH_URL = '/api/v1/auth/switch-tenant';

function tenant(slug: string, name: string, status = 'active'): object {
  return { id: `id-${slug}`, slug, name, status };
}

function membership(slug: string, name: string, status = 'active', tenantStatus = 'active'): object {
  return { id: `membership-${slug}`, tenant: tenant(slug, name, tenantStatus), role: 'admin', status };
}

/** Admin of tenant-a with active access to tenant-b, a suspended tenant and a suspended membership. */
function multiTenantView(activeSlug = 'tenant-a'): Record<string, unknown> {
  const names: Record<string, string> = { 'tenant-a': 'Tenant A', 'tenant-b': 'Tenant B' };

  return sessionView({
    activeTenant: tenant(activeSlug, names[activeSlug]),
    memberships: [membership('tenant-a', 'Tenant A'), membership('tenant-b', 'Tenant B'), membership('tenant-c', 'Tenant C', 'active', 'suspended'), membership('tenant-d', 'Tenant D', 'suspended')]
  });
}

describe('shell: tenant switcher and account menu', () => {
  let http: HttpTestingController;

  let router: Router;

  let messages: { add: ReturnType<typeof vi.fn> };

  let location: ReturnType<typeof fakeLocation>;

  beforeEach(() => {
    stubBroadcastChannel();
    stubMatchMedia();
    // jsdom does not implement scrollIntoView, which the open select calls.
    vi.stubGlobal('Element', Element);
    Element.prototype.scrollIntoView = vi.fn();
    messages = { add: vi.fn() };
    location = fakeLocation();
    configureFeatureTest(
      [
        { path: 'work', component: BlankComponent },
        { path: '', component: BlankComponent }
      ],
      [
        { provide: MessageService, useValue: messages },
        { provide: BROWSER_LOCATION, useValue: location }
      ]
    );
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    document.body.querySelectorAll('.p-select-overlay').forEach((overlay) => overlay.remove());
    http.verify();
    vi.unstubAllGlobals();
  });

  async function renderSwitcher(view: object | null = multiTenantView()): Promise<ComponentFixture<TenantSwitcher>> {
    await loadSession(view);
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

  describe('tenant switcher', () => {
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

      const request = await nextCall('POST', SWITCH_URL);

      expect(request.request.body).toEqual({ tenantId: 'id-tenant-b' });
      request.flush(okEnvelope(multiTenantView('tenant-b')));

      await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' })));
      await vi.waitFor(() => expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-b'));
      await vi.waitFor(() => expect(finishedUrls).toEqual(['/', '/work']));
      subscription.unsubscribe();
      expect(router.url).toBe('/work');
    });

    it.each([
      ['TENANT_SUSPENDED', 'Ese tenant está suspendido.'],
      ['TENANT_ACCESS_DENIED', 'No tienes acceso a ese tenant.'],
      ['MEMBERSHIP_REVOKED', 'Tu membresía en ese tenant fue revocada.']
    ])('keeps the old tenant and toasts the %s refusal when the switch is denied', async (code, text) => {
      const fixture = await renderSwitcher();

      const options = await openOptions(fixture);

      options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();
      (await nextCall('POST', SWITCH_URL)).flush(errorEnvelope(code), { status: 403, statusText: 'Forbidden' });
      (await nextCall('GET', SESSION_URL)).flush(okEnvelope(multiTenantView()));

      await vi.waitFor(() => expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', summary: text })));
      await settle(fixture);
      expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-a');
      expect(textOf(byTestId(fixture.nativeElement, 'tenant-switcher'))).toContain('Tenant A (tenant-a)');
    });

    it('keeps the old tenant without a second toast when the switch hits a server failure', async () => {
      const fixture = await renderSwitcher();

      const options = await openOptions(fixture);

      options.find((option) => option.getAttribute('data-key') === 'tenant-b')?.click();
      (await nextCall('POST', SWITCH_URL)).flush(errorEnvelope('UNAVAILABLE'), { status: 503, statusText: 'Service Unavailable' });

      await settle(fixture);
      expect(messages.add).not.toHaveBeenCalled();
      expect(textOf(byTestId(fixture.nativeElement, 'active-tenant-slug'))).toBe('tenant-a');
    });
  });

  describe('account menu', () => {
    async function renderMenu(view: object | null): Promise<ComponentFixture<AccountMenu>> {
      await loadSession(view);

      const fixture = TestBed.createComponent(AccountMenu);

      await settle(fixture);

      return fixture;
    }

    it('shows the email and the role of a tenant member', async () => {
      const fixture = await renderMenu(sessionView());

      expect(textOf(byTestId(fixture.nativeElement, 'session-account-email'))).toBe('admin@example.com');
      expect(byTestId(fixture.nativeElement, 'session-role')?.getAttribute('data-value')).toBe('admin');
      expect(byTestId(fixture.nativeElement, 'session-operator')).toBeNull();
    });

    it('shows the operator badge of a platform operator', async () => {
      const operator = { id: 'a', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true };

      const fixture = await renderMenu(sessionView({ account: operator, activeTenant: null, memberships: [] }));

      expect(textOf(byTestId(fixture.nativeElement, 'session-account-email'))).toBe('op@example.com');
      expect(byTestId(fixture.nativeElement, 'session-operator')).not.toBeNull();
      expect(byTestId(fixture.nativeElement, 'session-role')).toBeNull();
    });

    it('renders nothing for an anonymous visitor', async () => {
      const fixture = await renderMenu(null);

      expect(byTestId(fixture.nativeElement, 'session-account-email')).toBeNull();
      expect(byTestId(fixture.nativeElement, 'sign-out')).toBeNull();
    });

    it('signs out through the backend and follows the logout URL', async () => {
      const fixture = await renderMenu(sessionView());

      (byTestId(fixture.nativeElement, 'sign-out') as HTMLButtonElement).click();
      (await nextCall('POST', '/api/v1/auth/logout')).flush(okEnvelope({ logoutUrl: 'http://localhost/login' }));

      await vi.waitFor(() => expect(location.assign).toHaveBeenCalledWith('http://localhost/login'));
    });
  });
});
