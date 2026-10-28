import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { TableModule } from '@openng/optimus-ui/table';
import { ToggleSwitchModule } from '@openng/optimus-ui/toggleswitch';
import { firstValueFrom, map } from 'rxjs';
import { errorMessage, showsLoadError } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { ListStateComponent } from '@/core/ui/list-state/list-state.component';
import { PageHeaderComponent } from '@/core/ui/page-header/page-header.component';
import { InlineConfirmComponent, TenantsApi } from '@/platform/core';
import type { TenantView } from '@/platform/core';
import { FeatureFlagsApi } from '../feature-flags-api.service';
import type { FlagView } from '../feature-flags.types';
import { PLATFORM_FEATURE_FLAGS_I18N } from '../i18n/scope';

const SCOPE_ALIAS = PLATFORM_FEATURE_FLAGS_I18N.alias as string;

interface OverrideRow {
  readonly tenantId: string;
  readonly enabled: boolean;
  /** Tenant slug, or the tenant id when the tenant is not in the loaded list. */
  readonly dataKey: string;
  readonly name: string;
  readonly slug: string;
}

/** One flag with its tenant overrides; the tenant list resolves ids to names and feeds the picker. */
@Component({
  selector: 'app-flag-detail',
  imports: [FormsModule, ButtonModule, MessageModule, SelectModule, TableModule, ToggleSwitchModule, TranslocoModule, ListStateComponent, PageHeaderComponent, InlineConfirmComponent],
  providers: [provideTranslocoScope(PLATFORM_FEATURE_FLAGS_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './flag-detail.component.html'
})
export class FlagDetailPage {
  private readonly api = inject(FeatureFlagsApi);
  private readonly tenantsApi = inject(TenantsApi);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);
  private readonly route = inject(ActivatedRoute);

  private loadSequence = 0;
  private hasLoaded = false;

  protected readonly key = toSignal(this.route.paramMap.pipe(map((params) => params.get('key') ?? '')), { initialValue: this.route.snapshot?.paramMap?.get('key') ?? '' });

  protected readonly flag = signal<FlagView | null>(null);
  protected readonly tenants = signal<TenantView[]>([]);
  protected readonly tenantsFailed = signal(false);
  protected readonly isLoading = signal(false);
  protected readonly loadFailed = signal(false);
  protected readonly isSaving = signal(false);

  protected readonly pickedTenantId = signal<string | null>(null);
  protected readonly newEnabled = signal(true);

  protected readonly listedCount = signal(0);
  protected readonly totalItems = signal(0);
  protected readonly isNotFound = computed(() => !this.isLoading() && !this.loadFailed() && this.flag() === null);
  protected readonly isTruncated = computed(() => this.totalItems() > this.listedCount());

  protected readonly rows = computed<OverrideRow[]>(() => {
    const overrides = this.flag()?.tenantOverrides ?? [];

    const byId = new Map(this.tenants().map((tenant) => [tenant.id, tenant]));

    const rows = overrides.map((override) => {
      const tenant = byId.get(override.tenantId);

      return { tenantId: override.tenantId, enabled: override.enabled, dataKey: tenant?.slug ?? override.tenantId, name: tenant?.name ?? override.tenantId, slug: tenant?.slug ?? '' };
    });

    return rows;
  });

  constructor() {
    // Loads on first render and again whenever the route key changes.
    effect(() => {
      this.key();
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

  protected async save(): Promise<void> {
    const flag = this.flag();

    const tenantId = this.pickedTenantId();

    if (flag === null || tenantId === null || this.isSaving()) {
      return;
    }

    this.isSaving.set(true);

    try {
      const updated = await firstValueFrom(this.api.setTenant(flag.key, tenantId, this.newEnabled()));

      this.flag.set(updated);
      this.pickedTenantId.set(null);
      this.newEnabled.set(true);
    } catch (error) {
      this.fail(error);
      await this.load();
    } finally {
      this.isSaving.set(false);
    }
  }

  protected async remove(row: OverrideRow): Promise<void> {
    const flag = this.flag();

    if (flag === null) {
      return;
    }

    try {
      const updated = await firstValueFrom(this.api.clearTenant(flag.key, row.tenantId));

      this.flag.set(updated);
    } catch (error) {
      this.fail(error);
      await this.load();
    }
  }

  /** Error toast; the caller refetches so the screen shows what the server holds. */
  private fail(error: unknown): void {
    const failure = new SubmitErrors();

    failure.record(error);

    let summary: string;

    if (failure.code() === 'FLAG_NOT_DEFINED_IN_MANIFEST') {
      summary = this.transloco.translate(`${SCOPE_ALIAS}.errors.FLAG_NOT_DEFINED_IN_MANIFEST`);
    } else if (failure.status() === 404) {
      summary = this.transloco.translate(`${SCOPE_ALIAS}.errors.tenantGone`);
    } else {
      summary = errorMessage(this.transloco, error);
    }

    this.messages.add({ severity: 'error', summary });
  }

  private async load(): Promise<void> {
    this.loadSequence += 1;

    const sequence = this.loadSequence;

    this.isLoading.set(!this.hasLoaded);
    this.loadFailed.set(false);

    const tenantsRequest = firstValueFrom(this.tenantsApi.list()).then(
      (page) => ({ ok: true as const, items: page.items }),
      () => ({ ok: false as const, items: [] as TenantView[] })
    );

    try {
      const page = await firstValueFrom(this.api.list());

      const tenants = await tenantsRequest;

      if (sequence !== this.loadSequence) {
        return;
      }

      this.listedCount.set(page.items.length);
      this.totalItems.set(page.totalItems);
      this.flag.set(page.items.find((item) => item.key === this.key()) ?? null);
      this.tenants.set(tenants.items);
      this.tenantsFailed.set(!tenants.ok);
      this.hasLoaded = true;
    } catch (error) {
      if (sequence === this.loadSequence) {
        this.flag.set(null);
        this.loadFailed.set(showsLoadError(error));
      }
    } finally {
      if (sequence === this.loadSequence) {
        this.isLoading.set(false);
      }
    }
  }
}
