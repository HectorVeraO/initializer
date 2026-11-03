import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, viewChild } from '@angular/core';
import { TranslocoModule, provideTranslocoScope } from '@jsverse/transloco';
import { AvatarModule } from '@openng/optimus-ui/avatar';
import { ButtonModule } from '@openng/optimus-ui/button';
import { PopoverModule } from '@openng/optimus-ui/popover';
import type { Popover } from '@openng/optimus-ui/popover';
import { ACCOUNT_SLOT_OVERLAY } from '@/platform/core';
import { IDENTITY_I18N } from '../i18n/scope';
import { SessionStore } from '../session/session-store.service';

/**
 * Topbar account block: email, role or operator badge and Sign out. In a topbar
 * that marks its account slot as an overlay it renders a circular avatar that
 * opens a popover; otherwise the same content renders inline. Renders nothing
 * while the session is not authenticated.
 */
@Component({
  selector: 'app-account-menu',
  imports: [AvatarModule, ButtonModule, NgTemplateOutlet, PopoverModule, TranslocoModule],
  providers: [provideTranslocoScope(IDENTITY_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './account-menu.component.html'
})
export class AccountMenu {
  private readonly store = inject(SessionStore);
  private readonly accountPopover = viewChild<Popover>('accountPopover');

  protected readonly overlay = inject(ACCOUNT_SLOT_OVERLAY, { optional: true }) ?? false;
  protected readonly isAuthenticated = computed(() => this.store.state() === 'authenticated');
  protected readonly account = this.store.account;

  protected async signOut(): Promise<void> {
    this.accountPopover()?.hide();
    await this.store.logout();
  }
}
