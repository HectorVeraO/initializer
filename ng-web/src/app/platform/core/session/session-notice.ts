import { TranslocoService } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { firstValueFrom } from 'rxjs';
import { PLATFORM_CORE_I18N } from '../module';

/** Toast `core.session.tenantChanged`. Never rejects. */
export async function notifyTenantChanged(messages: MessageService, transloco: TranslocoService): Promise<void> {
  try {
    const text = await firstValueFrom(transloco.selectTranslate('session.tenantChanged', {}, PLATFORM_CORE_I18N));

    messages.add({ severity: 'info', summary: text });
  } catch {
    // The toast is a courtesy; a missing translation must not break the session flow.
  }
}
