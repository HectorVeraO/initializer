import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { TableModule } from '@openng/optimus-ui/table';
import { firstValueFrom } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { SantiagoDatePipe } from '@/core/format/santiago-date.pipe';
import { injectConfirmDestructive } from '@/core/ui/confirm-delete/confirm-destructive';
import { DataTableComponent } from '@/core/ui/data-table/data-table.component';
import { DataTableColumn } from '@/core/ui/data-table/data-table.types';
import { FormFieldComponent } from '@/core/ui/form-field/form-field.component';
import { FormSectionComponent } from '@/core/ui/form-section/form-section.component';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { RowAction, RowActionsComponent } from '@/core/ui/row-actions/row-actions.component';
import { SectionCardComponent } from '@/core/ui/section-card/section-card.component';
import { StatusTagComponent } from '@/core/ui/status-tag/status-tag.component';
import { TENANCY_I18N } from '../i18n/scope';
import { ACCESS_PROVIDER, FLAG_SOURCE, InlineConfirmComponent, OneTimeSecretDialogComponent, UI_RESTYLE_FLAG, fieldIssueMessages, unmatchedIssueMessages } from '@/platform/core';
import type { TenantRole } from '@/platform/core';
import { InvitationsApi } from './invitations-api.service';
import type { InvitationView } from './invitations-api.service';

const SCOPE_ALIAS = TENANCY_I18N.alias as string;

const FORM_FIELDS: readonly string[] = ['email', 'role'];

const ROLES: readonly TenantRole[] = ['admin', 'member', 'viewer'];

/** Codes of a failed create that the page shows under the email field. */
const EMAIL_CODES: readonly string[] = [ApiErrorCode.MembershipAlreadyActive, ApiErrorCode.PlatformOperatorRequired];

/** Admin screen of the pending invitations of the active tenant, and the form that creates one. */
@Component({
  selector: 'app-invitations-page',
  imports: [
    ReactiveFormsModule,
    ButtonModule,
    InputTextModule,
    MessageModule,
    SelectModule,
    TableModule,
    TranslocoModule,
    SantiagoDatePipe,
    DataTableComponent,
    DialogModule,
    FormFieldComponent,
    FormSectionComponent,
    ListStateComponent,
    PageHeaderComponent,
    RowActionsComponent,
    SectionCardComponent,
    StatusTagComponent,
    InlineConfirmComponent,
    OneTimeSecretDialogComponent
  ],
  providers: [provideTranslocoScope(TENANCY_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './invitations-page.component.html'
})
export class InvitationsPage {
  private readonly api = inject(InvitationsApi);
  private readonly access = inject(ACCESS_PROVIDER);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);
  private readonly flags = inject(FLAG_SOURCE);
  private readonly confirmDestructive = injectConfirmDestructive();

  private loadSequence = 0;
  private hasLoaded = false;

  /** Release flag: this page picks the restyled template or the previous one; a shared widget never reads it. */
  protected readonly restyled = computed(() => this.flags.isEnabled(UI_RESTYLE_FLAG));

  /** The restyled template invites in a dialog; the previous one keeps the inline form. */
  protected readonly isInviting = signal(false);

  protected readonly columns: DataTableColumn[] = [
    { field: 'email', labelKey: 'tenancy.invitations.email' },
    { field: 'role', labelKey: 'tenancy.invitations.role' },
    { field: 'status', labelKey: 'tenancy.invitations.status' },
    { field: 'expiresAt', labelKey: 'tenancy.invitations.expires' }
  ];

  protected readonly form = new FormGroup({
    email: new FormControl('', { nonNullable: true }),
    role: new FormControl<TenantRole>('member', { nonNullable: true })
  });

  protected readonly roleOptions = ROLES.map((role) => ({ value: role, labelKey: `tenancy.roles.${role}` }));

  protected readonly rows = signal<InvitationView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);
  protected readonly isSubmitting = signal(false);
  protected readonly submitErrors = new SubmitErrors();
  private readonly lastError = signal<unknown>(null);

  /** The invitation link of the last `201`. Lives here only; cleared when the dialog closes and never copied elsewhere. */
  protected readonly secret = signal<string | null>(null);

  private readonly activeTenantId = computed(() => this.access.context()?.activeTenant?.id ?? null);

  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);

  protected readonly emailMessages = computed(() => {
    const messages = fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'email');

    const code = this.submitErrors.code();

    const isCodeOnEmail = code !== null && EMAIL_CODES.includes(code) && this.submitErrors.status() !== 422;

    if (isCodeOnEmail) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  protected readonly roleMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'role'));

  protected readonly formMessages = computed(() => {
    const messages = unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), FORM_FIELDS);

    const status = this.submitErrors.status();

    const code = this.submitErrors.code();

    const isHandledElsewhere = status === null || status === 422 || (code !== null && EMAIL_CODES.includes(code));

    if (!isHandledElsewhere) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  constructor() {
    // Loads on first render and again whenever the active tenant changes.
    effect(() => {
      this.activeTenantId();
      untracked(() => {
        this.hasLoaded = false;
        void this.load();
      });
    });
  }

  protected reload(): void {
    this.hasLoaded = false;
    void this.load();
  }

  protected openInvite(): void {
    this.submitErrors.clear();
    this.lastError.set(null);
    this.isInviting.set(true);
  }

  protected onInviteVisibleChange(isVisible: boolean): void {
    if (!isVisible) {
      this.isInviting.set(false);
    }
  }

  /** One row action: revoke a pending invitation after the shared confirmation. */
  protected rowActions(invitation: InvitationView): RowAction[] {
    const actions: RowAction[] = [{ id: 'revoke', labelKey: 'tenancy.invitations.revoke', icon: 'pi pi-ban', testId: 'invitation-revoke', destructive: true, run: () => this.confirmRevoke(invitation) }];

    return actions;
  }

  protected async submit(): Promise<void> {
    if (this.isSubmitting()) {
      return;
    }

    this.isSubmitting.set(true);
    this.submitErrors.clear();
    this.lastError.set(null);

    try {
      const created = await firstValueFrom(this.api.create({ email: this.form.controls.email.value.trim(), role: this.form.controls.role.value }));

      this.secret.set(created.invitationUrl);
      this.form.controls.email.reset('');
      this.isInviting.set(false);
      await this.load();
    } catch (error) {
      this.lastError.set(error);
      this.submitErrors.record(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  protected async revoke(invitation: InvitationView): Promise<void> {
    try {
      await firstValueFrom(this.api.revoke(invitation.id));
      await this.load();
    } catch (error) {
      await this.load();
      this.notifyRowFailure(error);
    }
  }

  protected closeSecret(): void {
    this.secret.set(null);
  }

  private confirmRevoke(invitation: InvitationView): void {
    this.confirmDestructive({ acceptLabel: this.transloco.translate('tenancy.invitations.revoke') }, () => void this.revoke(invitation));
  }

  /** A `404` or `409` on a row means it is gone or no longer pending: the list was refetched, tell the person why the row vanished. */
  private notifyRowFailure(error: unknown): void {
    const failure = new SubmitErrors();

    failure.record(error);

    const isGone = failure.status() === 404 || failure.status() === 409;

    const summary = isGone ? this.transloco.translate('tenancy.errors.rowGone') : errorMessage(this.transloco, error);

    this.messages.add({ severity: 'warn', summary });
  }

  private async load(): Promise<void> {
    this.loadSequence += 1;

    const sequence = this.loadSequence;

    this.isLoading.set(!this.hasLoaded);
    this.loadFailed.set(false);

    try {
      const page = await firstValueFrom(this.api.listPending());

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
