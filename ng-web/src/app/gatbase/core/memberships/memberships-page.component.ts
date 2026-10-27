import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { MessageModule } from '@openng/optimus-ui/message';
import { ButtonModule } from '@openng/optimus-ui/button';
import { SelectModule } from '@openng/optimus-ui/select';
import { TableModule } from '@openng/optimus-ui/table';
import { firstValueFrom } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { GATBASE_CORE_I18N } from '../module';
import { SessionStore } from '../session/session-store.service';
import type { MembershipRole, MembershipStatus } from '../session/session.types';
import { InlineConfirmComponent } from '../ui/inline-confirm.component';
import { MembershipsApi } from './memberships-api.service';
import type { MemberView, MembershipChange } from './memberships-api.service';

const ROLES: readonly MembershipRole[] = ['admin', 'member', 'viewer'];

/** Members of the active tenant. Every role reads the list; only an admin sees the write controls. */
@Component({
  selector: 'app-memberships-page',
  imports: [FormsModule, ButtonModule, MessageModule, SelectModule, TableModule, TranslocoModule, ListStateComponent, PageHeaderComponent, InlineConfirmComponent],
  providers: [provideTranslocoScope(GATBASE_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './memberships-page.component.html'
})
export class MembershipsPage {
  private readonly api = inject(MembershipsApi);
  private readonly store = inject(SessionStore);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly roleOptions = ROLES.map((role) => ({ value: role, labelKey: `gatbaseCore.shell.roles.${role}` }));

  protected readonly rows = signal<MemberView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);

  /** Role a person just picked per membership id, shown until the save answers. */
  private readonly pickedRoles = signal<Readonly<Record<string, MembershipRole>>>({});

  /** Message per membership id of the last failed write on that row. */
  protected readonly rowErrors = signal<Readonly<Record<string, string>>>({});

  protected readonly canWrite = computed(() => this.store.role() === 'admin');
  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);

  constructor() {
    // Loads on first render and again whenever the active tenant changes.
    effect(() => {
      this.store.activeTenantId();
      untracked(() => {
        this.hasLoaded = false;
        this.pickedRoles.set({});
        this.rowErrors.set({});
        void this.load();
      });
    });
  }

  protected roleOf(row: MemberView): MembershipRole {
    const role = this.pickedRoles()[row.id] ?? row.role;

    return role;
  }

  protected reload(): void {
    this.hasLoaded = false;
    void this.load();
  }

  protected async changeRole(row: MemberView, role: MembershipRole): Promise<void> {
    if (role === row.role) {
      return;
    }

    this.pickedRoles.update((picked) => ({ ...picked, [row.id]: role }));
    await this.save(row, { role });
  }

  protected async setStatus(row: MemberView, status: MembershipStatus): Promise<void> {
    await this.save(row, { status });
  }

  private async save(row: MemberView, change: MembershipChange): Promise<void> {
    this.setRowError(row.id, null);

    const tenantAtStart = this.store.activeTenantId();

    try {
      const updated = await firstValueFrom(this.api.update(row.id, change));

      this.clearPicked(row.id);

      // The tenant changed while the write was in flight: the reload for the new tenant owns the list.
      const isStale = this.store.activeTenantId() !== tenantAtStart;

      if (isStale) {
        return;
      }

      const isGoneFromList = updated.status === 'revoked';

      if (isGoneFromList) {
        this.rows.update((rows) => rows.filter((item) => item.id !== row.id));
        this.totalItems.update((total) => Math.max(0, total - 1));
      } else {
        this.rows.update((rows) => rows.map((item) => (item.id === row.id ? updated : item)));
      }
    } catch (error) {
      this.clearPicked(row.id);

      // A failure that answers after a tenant switch belongs to the previous tenant: report nothing in the new one.
      const isStale = this.store.activeTenantId() !== tenantAtStart;

      if (isStale) {
        return;
      }

      await this.handleFailure(row, error);
    }
  }

  private async handleFailure(row: MemberView, error: unknown): Promise<void> {
    const failure = new SubmitErrors();

    failure.record(error);

    if (failure.conflictWith(ApiErrorCode.LastTenantAdmin)) {
      this.setRowError(row.id, this.transloco.translate('gatbaseCore.memberships.lastAdmin'));

      return;
    }

    if (failure.status() === 404) {
      await this.load();
      this.messages.add({ severity: 'warn', summary: this.transloco.translate('gatbaseCore.errors.rowGone') });

      return;
    }

    this.messages.add({ severity: 'error', summary: errorMessage(this.transloco, error) });
  }

  private clearPicked(id: string): void {
    this.pickedRoles.update((picked) => {
      const rest = { ...picked };

      delete rest[id];

      return rest;
    });
  }

  private setRowError(id: string, message: string | null): void {
    this.rowErrors.update((errors) => {
      const rest = { ...errors };

      delete rest[id];

      return message === null ? rest : { ...rest, [id]: message };
    });
  }

  private async load(): Promise<void> {
    this.loadSequence += 1;

    const sequence = this.loadSequence;

    this.isLoading.set(!this.hasLoaded);
    this.loadFailed.set(false);

    try {
      const page = await firstValueFrom(this.api.list());

      if (sequence !== this.loadSequence) {
        return;
      }

      this.rows.set(page.items);
      this.totalItems.set(page.totalItems);
      this.hasLoaded = true;
    } catch (error) {
      if (sequence === this.loadSequence) {
        this.rows.set([]);
        this.totalItems.set(0);
        this.loadFailed.set(showsLoadError(error));
      }
    } finally {
      if (sequence === this.loadSequence) {
        this.isLoading.set(false);
      }
    }
  }
}
