import { coreModule, platformInterceptors, platformRoutes, platformTopbar, providePlatform } from '@/platform/core';
import { PLATFORM_CORE_I18N } from './i18n/scope';

describe('coreModule', () => {
  it('contributes nothing but its translation scope', () => {
    expect(coreModule.id).toBe('core');
    expect(coreModule.shellRoutes).toEqual([]);
    expect(coreModule.publicRoutes).toEqual([]);
    expect(coreModule.nav).toEqual([]);
    expect(coreModule.i18n).toBe(PLATFORM_CORE_I18N);
    expect(coreModule.topbar ?? []).toEqual([]);
    expect(coreModule.interceptors ?? []).toEqual([]);
    expect(coreModule.ports).toBeUndefined();
  });

  it('is a valid console on its own', () => {
    expect(() => providePlatform([coreModule])).not.toThrow();
    expect(platformRoutes([coreModule])).toEqual({ shell: [], public: [] });
    expect(platformInterceptors([coreModule])).toEqual([]);
    expect(platformTopbar([coreModule], 'context')).toEqual([]);
    expect(platformTopbar([coreModule], 'account')).toEqual([]);
  });
});
