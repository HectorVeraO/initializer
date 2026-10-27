import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TranslocoModule, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { GATBASE_MODULES } from '../composition/web-module';
import { GATBASE_CORE_I18N } from '../module';
import { SessionStore } from '../session/session-store.service';

/** Shown to an authenticated person without an active tenant. */
@Component({
  selector: 'app-no-tenant-page',
  imports: [RouterLink, ButtonModule, TranslocoModule],
  providers: [provideTranslocoScope(GATBASE_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './no-tenant-page.component.html'
})
export class NoTenantPage {
  private readonly store = inject(SessionStore);
  private readonly modules = inject(GATBASE_MODULES, { optional: true }) ?? [];

  protected readonly account = this.store.account;
  protected readonly isOperator = this.store.isPlatformOperator;
  protected readonly hasFlagsEntry = computed(() => this.modules.some((module) => module.nav.some((entry) => entry.path === '/operator/feature-flags')));

  protected async signOut(): Promise<void> {
    await this.store.logout();
  }
}
