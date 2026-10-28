import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { byTestId, configureFeatureTest, settle, textOf } from '@/core/testing/ui-testing';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession, nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/session-testing';
import { PLATFORM_MODULES } from '../composition/web-module';
import type { WebModule } from '../composition/web-module';
import { coreModule } from '../module';
import { BROWSER_LOCATION } from '../session/browser-location';
import { NoTenantPage } from './no-tenant-page.component';

const OPERATOR = { id: 'a', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true };

const MEMBER = { id: 'a', email: 'jane@example.com', fullName: null, role: null, isPlatformOperator: false };

const FLAGS_MODULE: WebModule = { ...coreModule, id: 'feature-flags', nav: [{ id: 'flags', labelKey: 'nav.flags', icon: 'x', path: '/operator/feature-flags', requires: {} }] };

describe('NoTenantPage', () => {
  let location: ReturnType<typeof fakeLocation>;

  beforeEach(() => {
    stubBroadcastChannel();
    location = fakeLocation();
  });

  afterEach(() => {
    TestBed.inject(HttpTestingController).verify();
    vi.unstubAllGlobals();
  });

  async function render(account: object, modules: readonly WebModule[] = [coreModule]) {
    configureFeatureTest(
      [],
      [
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: location },
        { provide: PLATFORM_MODULES, useValue: modules }
      ]
    );
    await loadSession(sessionView({ account, activeTenant: null, memberships: [] }));

    const fixture = TestBed.createComponent(NoTenantPage);

    await settle(fixture);

    // The core translation scope loads asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Cerrar sesión');
    });

    return fixture.nativeElement as HTMLElement;
  }

  it('explains the situation and shows the account email', async () => {
    const root = await render(MEMBER);

    expect(textOf(byTestId(root, 'no-tenant'))).toContain('No eres miembro activo de ningún tenant');
    expect(textOf(byTestId(root, 'no-tenant-account-email'))).toBe('jane@example.com');
  });

  it('links to operator tenants only for a platform operator', async () => {
    const member = await render(MEMBER);

    expect(byTestId(member, 'no-tenant-operator-tenants')).toBeNull();
    TestBed.resetTestingModule();

    const operator = await render(OPERATOR);

    expect(byTestId(operator, 'no-tenant-operator-tenants')?.getAttribute('href')).toBe('/operator/tenants');
    expect(byTestId(operator, 'no-tenant-operator-flags')).toBeNull();
  });

  it('links to operator feature flags when that nav entry exists', async () => {
    const root = await render(OPERATOR, [coreModule, FLAGS_MODULE]);

    expect(byTestId(root, 'no-tenant-operator-flags')?.getAttribute('href')).toBe('/operator/feature-flags');
  });

  it('signs out', async () => {
    const root = await render(MEMBER);

    (byTestId(root, 'sign-out') as HTMLButtonElement).click();
    (await nextCall('POST', '/api/v1/auth/logout')).flush({ data: { logoutUrl: '/login' }, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: null });
    await vi.waitFor(() => expect(location.assign).toHaveBeenCalledWith('/login'));
  });
});
