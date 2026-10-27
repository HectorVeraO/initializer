import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { SelectModule } from '@openng/optimus-ui/select';
import { errorCode, isServerFailure } from '@/core/api/api-error';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { scopedText } from '../i18n/scoped-text';
import { GATBASE_CORE_I18N } from '../module';
import { SessionStore } from '../session/session-store.service';

/** Codes of a refused switch that have their own text under `gatbaseCore.errors`. */
const SWITCH_REFUSALS: readonly string[] = [ApiErrorCode.TenantAccessDenied, ApiErrorCode.TenantSuspended, ApiErrorCode.MembershipRevoked];

/** Topbar select of the active tenant. Renders nothing while the session is not authenticated. */
@Component({
  selector: 'app-tenant-switcher',
  imports: [FormsModule, SelectModule, TranslocoModule],
  providers: [provideTranslocoScope(GATBASE_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './tenant-switcher.component.html'
})
export class TenantSwitcher {
  private readonly store = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly messages = inject(MessageService);
  private readonly transloco = inject(TranslocoService);

  protected readonly isAuthenticated = computed(() => this.store.state() === 'authenticated');
  protected readonly options = computed(() => [...this.store.switchableMemberships()]);
  protected readonly activeTenant = this.store.activeTenant;
  protected readonly pendingTenantId = signal<string | null>(null);

  /** The id the select shows: the one just picked while a switch runs, else the store's active tenant. */
  protected readonly shownTenantId = computed(() => this.pendingTenantId() ?? this.store.activeTenantId());

  protected async onChange(tenantId: string | null): Promise<void> {
    if (tenantId === null || tenantId === this.store.activeTenantId()) {
      return;
    }

    this.pendingTenantId.set(tenantId);

    try {
      await this.store.switchTenant(tenantId);
    } catch (error) {
      this.pendingTenantId.set(null);
      await this.reportFailure(error);

      return;
    }

    this.pendingTenantId.set(null);
    this.messages.add({ severity: 'success', summary: await scopedText(this.transloco, 'shell.tenantSwitched') });

    const previousUrl = this.router.url;

    // A round trip through '/' makes the router run every guard of the current URL again.
    await this.router.navigateByUrl('/', { skipLocationChange: true });
    await this.router.navigateByUrl(previousUrl);
  }

  private async reportFailure(error: unknown): Promise<void> {
    // The interceptor already toasts a network or server failure.
    if (isServerFailure(error)) {
      return;
    }

    const code = errorCode(error);

    const key = code !== null && SWITCH_REFUSALS.includes(code) ? `errors.${code}` : 'errors.switchFailed';

    this.messages.add({ severity: 'error', summary: await scopedText(this.transloco, key) });
  }
}
