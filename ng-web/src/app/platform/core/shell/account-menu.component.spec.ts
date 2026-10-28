import { HttpTestingController } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { byTestId, configureFeatureTest, settle, textOf } from '@/core/testing/ui-testing';
import { okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import { fakeLocation, loadSession } from '../../../../testing/fixtures/session-testing';
import { nextCall, stubBroadcastChannel } from '../../../../testing/fixtures/http-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { AccountMenu } from './account-menu.component';

describe('AccountMenu', () => {
  let location: ReturnType<typeof fakeLocation>;

  beforeEach(() => {
    stubBroadcastChannel();
    location = fakeLocation();
    configureFeatureTest(
      [],
      [
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: location }
      ]
    );
  });

  afterEach(() => {
    TestBed.inject(HttpTestingController).verify();
    vi.unstubAllGlobals();
  });

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
