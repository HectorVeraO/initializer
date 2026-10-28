import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { okEnvelope } from '../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../testing/fixtures/session-view';
import { SESSION_URL, fakeLocation } from '../../../testing/fixtures/session-testing';
import { CONFIG_URL, nextCall, stubBroadcastChannel } from '../../../testing/fixtures/http-testing';
import { BROWSER_LOCATION } from './session/browser-location';
import { coreModule } from './module';

const MEMBER = { id: '22222222-2222-4222-8222-222222222222', email: 'member@example.com', fullName: null, role: 'member', isPlatformOperator: false };

describe('coreModule api-keys route', () => {
  let http: HttpTestingController;

  let router: Router;

  beforeEach(() => {
    stubBroadcastChannel();
    const apiKeysRoute = coreModule.shellRoutes.find((route) => route.path === 'api-keys');

    configureFeatureTest(
      [
        { path: 'api-keys', canMatch: apiKeysRoute?.canMatch, component: BlankComponent },
        { path: 'forbidden', component: BlankComponent }
      ],
      [
        { provide: MessageService, useValue: { add: vi.fn() } },
        { provide: BROWSER_LOCATION, useValue: fakeLocation() }
      ]
    );
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    http.verify();
    vi.unstubAllGlobals();
  });

  async function go(account: unknown): Promise<string> {
    const navigation = router.navigateByUrl('/api-keys');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
    (await nextCall('GET', SESSION_URL)).flush(okEnvelope(sessionView({ account })));
    await navigation;

    return router.url;
  }

  it('opens for an admin of the active tenant', async () => {
    expect(await go(sessionView()['account'])).toBe('/api-keys');
  });

  it('sends a member to /forbidden', async () => {
    expect(await go(MEMBER)).toBe('/forbidden');
  });
});
