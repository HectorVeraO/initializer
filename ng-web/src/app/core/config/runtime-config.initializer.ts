import { inject, provideAppInitializer } from '@angular/core';
import { RuntimeConfigService } from './runtime-config.service';

/** Loads `runtime-config.json` before the first navigation. */
export function provideRuntimeConfigInit() {
  return provideAppInitializer(async () => {
    const runtimeConfig = inject(RuntimeConfigService);

    await runtimeConfig.load();
  });
}
