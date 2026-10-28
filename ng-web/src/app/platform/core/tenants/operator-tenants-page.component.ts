import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { TableModule } from '@openng/optimus-ui/table';
import { firstValueFrom } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { SantiagoDatePipe } from '@/core/format/santiago-date.pipe';
import { FormSectionComponent } from '@/core/ui/form-section/form-section.component';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { PLATFORM_CORE_I18N } from '../module';
import type { TenantStatus } from '../session/session.types';
import { InlineConfirmComponent } from '../ui/inline-confirm.component';
import { fieldIssueMessages, unmatchedIssueMessages } from '../ui/issue-message';
import { OneTimeSecretDialogComponent } from '../ui/one-time-secret-dialog.component';
import { TenantsApi } from './tenants-api.service';
import type { TenantView } from './tenants-api.service';

const SCOPE_ALIAS = PLATFORM_CORE_I18N.alias as string;

const FORM_FIELDS: readonly string[] = ['slug', 'name', 'adminEmail'];

/** Operator screen: every tenant, the form that creates one with its first-admin invitation, suspend and activate. */
@Component({
  selector: 'app-operator-tenants-page',
  imports: [ReactiveFormsModule, ButtonModule, InputTextModule, MessageModule, TableModule, TranslocoModule, SantiagoDatePipe, FormSectionComponent, ListStateComponent, PageHeaderComponent, InlineConfirmComponent, OneTimeSecretDialogComponent],
  providers: [provideTranslocoScope(PLATFORM_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './operator-tenants-page.component.html'
})
export class OperatorTenantsPage {
  private readonly api = inject(TenantsApi);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly form = new FormGroup({
    slug: new FormControl('', { nonNullable: true }),
    name: new FormControl('', { nonNullable: true }),
    adminEmail: new FormControl('', { nonNullable: true })
  });

  protected readonly rows = signal<TenantView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);
  protected readonly isSubmitting = signal(false);
  protected readonly submitErrors = new SubmitErrors();
  private readonly lastError = signal<unknown>(null);

  /** The invitation link of the last `201`. Lives here only; cleared when the dialog closes and never copied elsewhere. */
  protected readonly secret = signal<string | null>(null);

  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);

  protected readonly slugMessages = computed(() => {
    const messages = fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'slug');

    if (this.submitErrors.conflictWith(ApiErrorCode.TenantSlugTaken)) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  protected readonly nameMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'name'));

  protected readonly adminEmailMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'adminEmail'));

  protected readonly formMessages = computed(() => {
    const messages = unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), FORM_FIELDS);

    const status = this.submitErrors.status();

    const isHandledElsewhere = status === null || status === 422 || this.submitErrors.conflictWith(ApiErrorCode.TenantSlugTaken);

    if (!isHandledElsewhere) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  constructor() {
    void this.load();
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
      const value = this.form.getRawValue();

      const created = await firstValueFrom(this.api.create({ slug: value.slug.trim(), name: value.name.trim(), adminEmail: value.adminEmail.trim() }));

      this.secret.set(created.invitationUrl);
      this.form.reset({ slug: '', name: '', adminEmail: '' });
      await this.load();
    } catch (error) {
      this.lastError.set(error);
      this.submitErrors.record(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  protected async setStatus(tenant: TenantView, status: TenantStatus): Promise<void> {
    try {
      const updated = await firstValueFrom(this.api.setStatus(tenant.id, status));

      this.rows.update((rows) => rows.map((row) => (row.id === tenant.id ? updated : row)));
    } catch (error) {
      await this.load();
      this.notifyRowFailure(error);
    }
  }

  protected closeSecret(): void {
    this.secret.set(null);
  }

  /** A `404` or `409` on a row means it changed under the person: the list was refetched, say why. */
  private notifyRowFailure(error: unknown): void {
    const failure = new SubmitErrors();

    failure.record(error);

    const isGone = failure.status() === 404 || failure.status() === 409;

    const summary = isGone ? this.transloco.translate('core.errors.rowGone') : errorMessage(this.transloco, error);

    this.messages.add({ severity: 'warn', summary });
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
