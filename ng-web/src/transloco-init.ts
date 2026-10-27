import { inject, provideAppInitializer } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';
import { Optimus } from '@openng/optimus-ui/config';
import { firstValueFrom } from 'rxjs';

export function provideTranslocoInit() {
  return provideAppInitializer(() => {
    const transloco = inject(TranslocoService);

    const optimus = inject(Optimus);

    const bootstrapLang = 'es';

    const syncOptimusTranslations = async (lang: string) => {
      await firstValueFrom(transloco.load(lang));

      const optimusTranslations = transloco.translateObject('optimus', {}, lang);

      if (optimusTranslations && typeof optimusTranslations === 'object') {
        optimus.setTranslation(optimusTranslations);
      }
    };

    transloco.langChanges$.subscribe((lang) => {
      void syncOptimusTranslations(lang);
    });

    transloco.setActiveLang(bootstrapLang);

    return syncOptimusTranslations(bootstrapLang);
  });
}
