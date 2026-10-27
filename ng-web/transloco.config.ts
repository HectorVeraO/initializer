import { TRANSLOCO_CONFIG, TranslocoConfig } from '@jsverse/transloco';
import { TranslocoGlobalConfig } from '@jsverse/transloco-utils';

const config: TranslocoGlobalConfig = {
  rootTranslationsPath: 'src/assets/i18n/',
  langs: ['es', 'en'],
  defaultLang: 'es',
  keysManager: {}
};

export const translocoConfigProvider = {
  provide: TRANSLOCO_CONFIG,
  useValue: {
    availableLangs: config.langs,
    defaultLang: config.defaultLang,
    fallbackLang: 'es',
    reRenderOnLangChange: true,
    prodMode: false
  } as TranslocoConfig
};
