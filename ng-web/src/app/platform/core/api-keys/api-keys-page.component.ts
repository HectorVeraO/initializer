import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { FormControl, FormGroup, FormsModule, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { CheckboxModule } from '@openng/optimus-ui/checkbox';
import { DatePickerModule } from '@openng/optimus-ui/datepicker';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { TableModule } from '@openng/optimus-ui/table';
import { firstValueFrom } from 'rxjs';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { SantiagoDatePipe } from '@/core/format/santiago-date.pipe';
import { FormSectionComponent } from '@/core/ui/form-section/form-section.component';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { PLATFORM_CORE_I18N } from '../module';
import { SessionStore } from '../session/session-store.service';
import { InlineConfirmComponent } from '../ui/inline-confirm.component';
import { fieldIssueMessages, unmatchedIssueMessages } from '../ui/issue-message';
import { OneTimeSecretDialogComponent } from '../ui/one-time-secret-dialog.component';
import { ApiKeysApi } from './api-keys-api.service';
import type { ApiKeyScope, ApiKeyView } from './api-keys-api.service';

const SCOPE_ALIAS = PLATFORM_CORE_I18N.alias as string;

const FORM_FIELDS: readonly string[] = ['name', 'scopes', 'expiresAt'];

/** The chosen calendar day as the end of that day in UTC (`YYYY-MM-DDT23:59:59Z`), the only expiry rule the client has. */
export function endOfDayUtc(day: Date): string {
  const year = String(day.getFullYear()).padStart(4, '0');

  const month = String(day.getMonth() + 1).padStart(2, '0');

  const date = String(day.getDate()).padStart(2, '0');

  const timestamp = `${year}-${month}-${date}T23:59:59Z`;

  return timestamp;
}

/** Admin screen of the API keys of the active tenant, and the form that creates one. */
@Component({
  selector: 'app-api-keys-page',
  imports: [
    ReactiveFormsModule,
    FormsModule,
    ButtonModule,
    CheckboxModule,
    DatePickerModule,
    InputTextModule,
    MessageModule,
    TableModule,
    TranslocoModule,
    SantiagoDatePipe,
    FormSectionComponent,
    ListStateComponent,
    PageHeaderComponent,
    InlineConfirmComponent,
    OneTimeSecretDialogComponent
  ],
  providers: [provideTranslocoScope(PLATFORM_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './api-keys-page.component.html'
})
export class ApiKeysPage {
  private readonly api = inject(ApiKeysApi);
  private readonly store = inject(SessionStore);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly form = new FormGroup({
    name: new FormControl('', { nonNullable: true }),
    expiresAt: new FormControl<Date | null>(null)
  });

  protected readonly availableScopes = signal<readonly ApiKeyScope[]>([]);
  protected readonly selectedScopes = signal<readonly string[]>([]);
  protected readonly scopesFailed = signal(false);

  protected readonly rows = signal<ApiKeyView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);
  protected readonly isSubmitting = signal(false);
  protected readonly submitErrors = new SubmitErrors();
  private readonly lastError = signal<unknown>(null);

  /** The plain key of the last `201`. Lives here only; cleared when the dialog closes and never copied elsewhere. */
  protected readonly secret = signal<string | null>(null);

  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);
  protected readonly hasNoScopes = computed(() => this.availableScopes().length === 0);
  protected readonly isScopesEmpty = computed(() => this.hasNoScopes() && !this.scopesFailed());

  protected readonly nameMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'name'));
  protected readonly scopesMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'scopes'));
  protected readonly expiresAtMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'expiresAt'));

  protected readonly formMessages = computed(() => {
    const messages = unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), FORM_FIELDS);

    const status = this.submitErrors.status();

    const isHandledElsewhere = status === null || status === 422;

    if (!isHandledElsewhere) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  constructor() {
    // Loads on first render and again whenever the active tenant changes.
    effect(() => {
      this.store.activeTenantId();
      untracked(() => {
        this.hasLoaded = false;
        void this.loadScopes();
        void this.load();
      });
    });
  }

  protected reloadScopes(): void {
    void this.loadScopes();
  }

  protected reload(): void {
    this.hasLoaded = false;
    void this.load();
  }

  protected isScopeSelected(name: string): boolean {
    const isSelected = this.selectedScopes().includes(name);

    return isSelected;
  }

  protected toggleScope(name: string, isChecked: boolean): void {
    const others = this.selectedScopes().filter((scope) => scope !== name);

    const next = isChecked ? [...others, name] : others;

    this.selectedScopes.set(next);
  }

  protected async submit(): Promise<void> {
    if (this.isSubmitting() || this.hasNoScopes()) {
      return;
    }

    this.isSubmitting.set(true);
    this.submitErrors.clear();
    this.lastError.set(null);

    try {
      const expiry = this.form.controls.expiresAt.value;

      const created = await firstValueFrom(
        this.api.create({
          name: this.form.controls.name.value,
          scopes: this.selectedScopes(),
          expiresAt: expiry === null ? null : endOfDayUtc(expiry)
        })
      );

      this.secret.set(created.plainTextKey);
      this.form.reset({ name: '', expiresAt: null });
      this.selectedScopes.set([]);
      await this.load();
    } catch (error) {
      this.lastError.set(error);
      this.submitErrors.record(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  protected async revoke(key: ApiKeyView): Promise<void> {
    try {
      await firstValueFrom(this.api.revoke(key.id));
      await this.load();
    } catch (error) {
      await this.load();
      this.notifyRowFailure(error);
    }
  }

  protected closeSecret(): void {
    this.secret.set(null);
  }

  /** A `404` on a row means it is gone: the list was refetched, tell the person why the row vanished. */
  private notifyRowFailure(error: unknown): void {
    const failure = new SubmitErrors();

    failure.record(error);

    const isGone = failure.status() === 404;

    const summary = isGone ? this.transloco.translate('core.errors.rowGone') : errorMessage(this.transloco, error);

    this.messages.add({ severity: 'warn', summary });
  }

  /** The scope list comes from the server; a failed load leaves it empty, which disables the form and offers a retry. */
  private async loadScopes(): Promise<void> {
    this.scopesFailed.set(false);

    try {
      const scopes = await firstValueFrom(this.api.scopes());

      this.availableScopes.set(scopes);
      this.selectedScopes.set(this.selectedScopes().filter((name) => scopes.some((scope) => scope.name === name)));
    } catch {
      this.scopesFailed.set(true);
      this.availableScopes.set([]);
      this.selectedScopes.set([]);
    }
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
