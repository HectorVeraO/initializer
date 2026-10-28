import { TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { ACCESS_PROVIDER, FLAG_SOURCE, coreModule, platformRoutes, providePlatform } from '@/platform/core';
import { stubBroadcastChannel } from '../../../testing/fixtures/http-testing';
import { SessionAccess } from './access/session-access';
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

  it('binds the access and flag ports to one session-backed instance', () => {
    stubBroadcastChannel();
    configureFeatureTest([], [{ provide: MessageService, useValue: { add: vi.fn() } }]);
    TestBed.configureTestingModule({ providers: [providePlatform([coreModule, identityModule])] });

    expect(TestBed.inject(ACCESS_PROVIDER)).toBeInstanceOf(SessionAccess);
    expect(TestBed.inject(FLAG_SOURCE)).toBe(TestBed.inject(ACCESS_PROVIDER));
  });

  it('contributes the session interceptor', () => {
    expect(identityModule.interceptors).toHaveLength(1);
  });
});
