import { TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { ACCESS_PROVIDER, FLAG_SOURCE, coreModule, platformRoutes, providePlatform } from '@/platform/core';
import { stubBroadcastChannel } from '../../../testing/fixtures/http-testing';
import { loadSession } from './testing/session-testing';
import { sessionView } from './testing/session-view';
import { identityModule } from './module';

describe('identityModule', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('mounts /login outside the shell and adds no shell route', () => {
    const routes = platformRoutes([coreModule, identityModule]);

    expect(routes.public.map((route) => route.path)).toEqual(['login']);
    expect(routes.shell).toEqual([]);
  });

  it('adds no menu entry and puts the account menu in the account slot', () => {
    expect(identityModule.nav).toEqual([]);
    expect((identityModule.topbar ?? []).map((item) => `${item.id}:${item.slot}`)).toEqual(['account-menu:account']);
  });

  it('binds the access and flag ports to the signed-in session', async () => {
    stubBroadcastChannel();
    configureFeatureTest([], [{ provide: MessageService, useValue: { add: vi.fn() } }]);
    TestBed.configureTestingModule({ providers: [providePlatform([coreModule, identityModule])] });

    await loadSession(sessionView({ flags: { beta: true } }));

    expect(TestBed.inject(ACCESS_PROVIDER).context()?.account.email).toBe('admin@example.com');
    expect(TestBed.inject(FLAG_SOURCE).isEnabled('beta')).toBe(true);
    expect(TestBed.inject(FLAG_SOURCE).isEnabled('unknown')).toBe(false);
  });
});
