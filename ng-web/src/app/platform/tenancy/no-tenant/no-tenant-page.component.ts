import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { RouterLink } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { ACCESS_PROVIDER, FLAG_SOURCE, PLATFORM_MODULES, TENANT_SELECTION, allowsAccess, navLabel } from '@/platform/core';
import { TENANCY_I18N } from '../i18n/scope';

interface AllowedEntry {
  readonly id: string;
  readonly path: string;
  readonly label: string;
}

/** Shown to an authenticated person without an active tenant: what they can still open, then Sign out. */
@Component({
  selector: 'app-no-tenant-page',
  imports: [RouterLink, ButtonModule, TranslocoModule],
  providers: [provideTranslocoScope(TENANCY_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './no-tenant-page.component.html'
})
export class NoTenantPage {
  private readonly access = inject(ACCESS_PROVIDER);
  private readonly flags = inject(FLAG_SOURCE);
  private readonly isMultiTenant = inject(TENANT_SELECTION) !== null;
  private readonly modules = inject(PLATFORM_MODULES, { optional: true }) ?? [];
  private readonly transloco = inject(TranslocoService);

  /** Label per `<module>/<entry>` key, kept current with the active language. */
  private readonly labels = signal<Readonly<Record<string, string>>>({});

  protected readonly account = computed(() => this.access.context()?.account ?? null);

  /** Every nav entry of every installed module the current tenantless visitor may open, in module order. */
  protected readonly allowedEntries = computed<AllowedEntry[]>(() => {
    const labels = this.labels();

    const entries = this.modules.flatMap((module) =>
      module.nav.filter((entry) => allowsAccess(this.access, this.flags, this.isMultiTenant, entry.requires)).map((entry) => ({ id: entry.id, path: entry.path, label: labels[`${module.id}/${entry.id}`] ?? entry.labelKey }))
    );

    return entries;
  });

  constructor() {
    for (const module of this.modules) {
      for (const entry of module.nav) {
        navLabel(this.transloco, module, entry)
          .pipe(takeUntilDestroyed())
          .subscribe((label) => {
            this.labels.update((labels) => ({ ...labels, [`${module.id}/${entry.id}`]: label }));
          });
      }
    }
  }

  protected async signOut(): Promise<void> {
    await this.access.signOut();
  }
}
