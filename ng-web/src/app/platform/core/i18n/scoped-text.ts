import { ProviderScope, TranslocoService } from '@jsverse/transloco';
import { firstValueFrom } from 'rxjs';

/** Text of a key relative to `scope` (`shell.signOut`), loading the scope when needed. Resolves with the key itself when the scope cannot be loaded. */
export async function scopedText(transloco: TranslocoService, scope: ProviderScope, key: string, params: Record<string, unknown> = {}): Promise<string> {
  try {
    const text = await firstValueFrom(transloco.selectTranslate(key, params, scope));

    return text;
  } catch {
    return key;
  }
}
