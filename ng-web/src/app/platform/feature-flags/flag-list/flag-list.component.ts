import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { TableModule } from '@openng/optimus-ui/table';
import { Observable, firstValueFrom } from 'rxjs';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { FeatureFlagsApi } from '../feature-flags-api.service';
import type { FlagView } from '../feature-flags.types';
import { PLATFORM_FEATURE_FLAGS_I18N } from '../i18n/scope';

const SCOPE_ALIAS = PLATFORM_FEATURE_FLAGS_I18N.alias as string;

/** The three states of the global override of a flag. */
export type GlobalChoice = 'inherit' | 'on' | 'off';

interface GlobalOption {
  readonly value: GlobalChoice;
  readonly labelKey: string;
}

/** The flags of the manifest with the global override select; the detail holds the tenant overrides. */
@Component({
  selector: 'app-flag-list',
  imports: [FormsModule, RouterLink, MessageModule, SelectModule, TableModule, TranslocoModule, ListStateComponent, PageHeaderComponent],
  providers: [provideTranslocoScope(PLATFORM_FEATURE_FLAGS_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './flag-list.component.html'
})
export class FlagListPage {
  private readonly api = inject(FeatureFlagsApi);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly options: GlobalOption[] = [
    { value: 'inherit', labelKey: `${SCOPE_ALIAS}.global.inherit` },
    { value: 'on', labelKey: `${SCOPE_ALIAS}.global.on` },
    { value: 'off', labelKey: `${SCOPE_ALIAS}.global.off` }
  ];

  protected readonly rows = signal<FlagView[]>([]);
  protected readonly totalItems = signal(0);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);

  /** Choices the person made that the server has not confirmed yet, by flag key. */
  protected readonly picked = signal<Readonly<Record<string, GlobalChoice>>>({});

  protected readonly isEmpty = computed(() => !this.isLoading() && !this.loadFailed() && this.rows().length === 0);
  protected readonly isTruncated = computed(() => this.totalItems() > this.rows().length);

  constructor() {
    void this.load();
  }

  protected reload(): void {
    this.hasLoaded = false;
    void this.load();
  }

  protected choiceOf(row: FlagView): GlobalChoice {
    const picked = this.picked()[row.key];

    if (picked !== undefined) {
      return picked;
    }

    if (row.globalOverride === null) {
      return 'inherit';
    }

    return row.globalOverride.enabled ? 'on' : 'off';
  }

  protected async change(row: FlagView, choice: GlobalChoice): Promise<void> {
    if (choice === this.choiceOf(row)) {
      return;
    }

    this.picked.update((picked) => ({ ...picked, [row.key]: choice }));

    let call: Observable<FlagView>;

    if (choice === 'inherit') {
      call = this.api.clearGlobal(row.key);
    } else {
      call = this.api.setGlobal(row.key, choice === 'on');
    }

    try {
      const updated = await firstValueFrom(call);

      this.rows.update((rows) => rows.map((item) => (item.key === updated.key ? updated : item)));
      this.clearPicked(row.key);
    } catch (error) {
      this.clearPicked(row.key);
      this.messages.add({ severity: 'error', summary: this.failureText(error) });

      // The server is the source of truth after a failure (a deploy race can remove the flag).
      await this.load();
    }
  }

  private clearPicked(key: string): void {
    const next = { ...this.picked() };

    delete next[key];
    this.picked.set(next);
  }

  private failureText(error: unknown): string {
    const failure = new SubmitErrors();

    failure.record(error);

    if (failure.code() === 'FLAG_NOT_DEFINED_IN_MANIFEST') {
      return this.transloco.translate(`${SCOPE_ALIAS}.errors.FLAG_NOT_DEFINED_IN_MANIFEST`);
    }

    return errorMessage(this.transloco, error);
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
