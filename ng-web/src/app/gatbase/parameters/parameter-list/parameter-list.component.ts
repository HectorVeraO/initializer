import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { MessageModule } from '@openng/optimus-ui/message';
import { TableModule } from '@openng/optimus-ui/table';
import { TagModule } from '@openng/optimus-ui/tag';
import { firstValueFrom } from 'rxjs';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { InlineConfirmComponent, SessionStore } from '@/gatbase/core';
import { displayValue, jsonText } from '../json-value';
import { GATBASE_PARAMETERS_I18N } from '../module';
import { ParameterDefinitionDialog } from '../parameter-definition-dialog/parameter-definition-dialog.component';
import { ParameterOverrideDialog } from '../parameter-override-dialog/parameter-override-dialog.component';
import { ParametersApi } from '../parameters-api.service';
import type { ParameterView } from '../parameters.types';

const SCOPE_ALIAS = GATBASE_PARAMETERS_I18N.alias as string;

/** The parameters of the platform with the value in effect for the active tenant; admins override it, platform operators define parameters. */
@Component({
  selector: 'app-parameter-list',
  imports: [ButtonModule, MessageModule, TableModule, TagModule, TranslocoModule, ListStateComponent, PageHeaderComponent, InlineConfirmComponent, ParameterOverrideDialog, ParameterDefinitionDialog],
  providers: [provideTranslocoScope(GATBASE_PARAMETERS_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './parameter-list.component.html'
})
export class ParameterListPage {
  private readonly api = inject(ParametersApi);
  private readonly store = inject(SessionStore);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly rows = signal<ParameterView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);

  /** Row-level failure texts by parameter key. */
  protected readonly rowMessages = signal<Readonly<Record<string, string>>>({});

  protected readonly overrideTarget = signal<ParameterView | null>(null);
  protected readonly isDefinitionOpen = signal(false);
  protected readonly definitionTarget = signal<ParameterView | null>(null);

  protected readonly canOverride = computed(() => this.store.role() === 'admin' && this.store.activeTenantId() !== null);
  protected readonly isOperator = this.store.isPlatformOperator;
  protected readonly showsActions = computed(() => this.canOverride() || this.isOperator());

  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);

  constructor() {
    // Loads on first render and again whenever the active tenant changes.
    effect(() => {
      this.store.activeTenantId();
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

  protected valueOf(row: ParameterView): string {
    const text = displayValue(row.effectiveValue);

    return text;
  }

  protected defaultOf(row: ParameterView): string {
    const text = displayValue(row.defaultValue);

    return text;
  }

  protected boundsOf(row: ParameterView): string {
    if (row.allowedValues !== null && row.allowedValues.length > 0) {
      const values = row.allowedValues.map((value) => displayValue(value)).join(', ');

      return this.transloco.translate(`${SCOPE_ALIAS}.list.allowed`, { values });
    }

    if (row.minValue === null && row.maxValue === null) {
      return this.transloco.translate(`${SCOPE_ALIAS}.list.noBounds`);
    }

    const min = row.minValue === null ? '' : jsonText(row.minValue);

    const max = row.maxValue === null ? '' : jsonText(row.maxValue);

    return this.transloco.translate(`${SCOPE_ALIAS}.list.range`, { min, max });
  }

  protected openOverride(row: ParameterView): void {
    this.overrideTarget.set(row);
  }

  protected closeOverride(): void {
    this.overrideTarget.set(null);
  }

  protected openDefinition(row: ParameterView | null): void {
    this.definitionTarget.set(row);
    this.isDefinitionOpen.set(true);
  }

  protected closeDefinition(): void {
    this.isDefinitionOpen.set(false);
  }

  /** A dialog saved: show what the server holds now. */
  protected onSaved(): void {
    this.rowMessages.set({});
    this.reload();
  }

  protected async clearOverride(row: ParameterView): Promise<void> {
    await this.runRowAction(row, async () => {
      await firstValueFrom(this.api.clearOverride(row.key));
    });
  }

  protected async remove(row: ParameterView): Promise<void> {
    await this.runRowAction(row, async () => {
      const deleted = await firstValueFrom(this.api.remove(row.key));

      const summary = this.transloco.translate(`${SCOPE_ALIAS}.messages.deleted`, { key: deleted.key, count: deleted.deletedOverrideCount });

      this.messages.add({ severity: 'success', summary });
    });
  }

  /** Runs a write on one row, then refetches. A failure becomes a message on that row; a `404` says the row is gone. */
  private async runRowAction(row: ParameterView, action: () => Promise<void>): Promise<void> {
    this.setRowMessage(row.key, null);

    try {
      await action();
      await this.load();
    } catch (error) {
      await this.load();
      this.setRowMessage(row.key, this.failureText(error));
    }
  }

  private failureText(error: unknown): string {
    const failure = new SubmitErrors();

    failure.record(error);

    const code = failure.code();

    const key = `${SCOPE_ALIAS}.errors.${code}`;

    const translated = code === null ? key : this.transloco.translate(key);

    if (failure.status() === 404) {
      return this.transloco.translate(`${SCOPE_ALIAS}.errors.rowGone`);
    }

    const isKnown = code !== null && translated !== key;

    return isKnown ? translated : errorMessage(this.transloco, error);
  }

  private setRowMessage(key: string, message: string | null): void {
    const next = { ...this.rowMessages() };

    if (message === null) {
      delete next[key];
    } else {
      next[key] = message;
    }

    this.rowMessages.set(next);
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
