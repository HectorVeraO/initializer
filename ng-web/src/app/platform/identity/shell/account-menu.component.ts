import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { TranslocoModule, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { IDENTITY_I18N } from '../i18n/scope';
import { SessionStore } from '../session/session-store.service';

/** Topbar account block: email, role or operator badge and Sign out. Renders nothing while the session is not authenticated. */
@Component({
  selector: 'app-account-menu',
  imports: [ButtonModule, TranslocoModule],
  providers: [provideTranslocoScope(IDENTITY_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './account-menu.component.html'
})
export class AccountMenu {
  private readonly store = inject(SessionStore);

  protected readonly isAuthenticated = computed(() => this.store.state() === 'authenticated');
  protected readonly account = this.store.account;

  protected async signOut(): Promise<void> {
    await this.store.logout();
  }
}
