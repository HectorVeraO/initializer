import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { TableModule } from '@openng/optimus-ui/table';
import { firstValueFrom } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { SantiagoDatePipe } from '@/core/format/santiago-date.pipe';
import { FormSectionComponent } from '@/core/ui/form-section/form-section.component';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { TENANCY_I18N } from '../i18n/tenancy/scope';
import { ACCESS_PROVIDER } from '../access/access.port';
import type { TenantRole } from '../access/access.types';
import { InlineConfirmComponent } from '../ui/inline-confirm.component';
import { fieldIssueMessages, unmatchedIssueMessages } from '../ui/issue-message';
import { OneTimeSecretDialogComponent } from '../ui/one-time-secret-dialog.component';
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
    FormSectionComponent,
    ListStateComponent,
    PageHeaderComponent,
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

  private loadSequence = 0;
  private hasLoaded = false;

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
