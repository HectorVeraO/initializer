import { TranslocoService } from '@jsverse/transloco';
import { firstValueFrom } from 'rxjs';
import { GATBASE_CORE_I18N } from '../module';

/** Text of a key relative to the core scope (`shell.signOut`), loading the scope when needed. Resolves with the key itself when the scope cannot be loaded. */
export async function scopedText(transloco: TranslocoService, key: string, params: Record<string, unknown> = {}): Promise<string> {
  try {
    const text = await firstValueFrom(transloco.selectTranslate(key, params, GATBASE_CORE_I18N));

    return text;
  } catch {
    return key;
  }
}
