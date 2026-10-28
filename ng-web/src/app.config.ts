import { ApplicationConfig, ErrorHandler, LOCALE_ID, isDevMode } from '@angular/core';
import { registerLocaleData } from '@angular/common';
import localeEsCL from '@angular/common/locales/es-CL';
registerLocaleData(localeEsCL);
import { provideRouter, withEnabledBlockingInitialNavigation, withInMemoryScrolling, withRouterConfig } from '@angular/router';
import { provideOptimus } from '@openng/optimus-ui/config';
import { appRoutes } from './app.routes';
import { ConfirmationService, MessageService } from '@openng/optimus-ui/api';
import { GlobalErrorHandler } from '@/core/handler/global-error.handler';
import { provideApiHttpClient } from '@/core/interceptors/api-http.providers';
import { platformInterceptors, providePlatform } from '@/platform/core';
import { installedModules } from '@/platform/modules';
import { provideRuntimeConfigInit } from '@/core/config/runtime-config.initializer';
import { provideTransloco } from '@jsverse/transloco';
import { TranslocoHttpLoader } from './transloco-loader';
import { provideTranslocoInit } from './transloco-init';
import { AppPreset } from './app-theme.preset';

export const appConfig: ApplicationConfig = {
  providers: [
    provideRouter(appRoutes, withInMemoryScrolling({ anchorScrolling: 'enabled', scrollPositionRestoration: 'enabled' }), withEnabledBlockingInitialNavigation(), withRouterConfig({ onSameUrlNavigation: 'reload' })),
    provideApiHttpClient(platformInterceptors(installedModules)),
    providePlatform(installedModules),
    provideOptimus({
      theme: { preset: AppPreset, options: { darkModeSelector: '.app-dark' } }
    }),
    MessageService,
    ConfirmationService,
    { provide: LOCALE_ID, useValue: 'es-CL' },
    { provide: ErrorHandler, useClass: GlobalErrorHandler },
    provideTransloco({
      config: {
        availableLangs: ['es', 'en'],
        defaultLang: 'es',
        fallbackLang: 'es',
        reRenderOnLangChange: true,
        prodMode: !isDevMode()
      },
      loader: TranslocoHttpLoader
    }),
    provideTranslocoInit(),
    provideRuntimeConfigInit()
  ]
};
