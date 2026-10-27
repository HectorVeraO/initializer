import { Component, OnDestroy, OnInit, computed, inject, ChangeDetectionStrategy, signal } from '@angular/core';
import { RouterModule } from '@angular/router';
import { MenuItem } from '@openng/optimus-ui/api';
import { AppMenuitem } from '../menu-item/app.menuitem';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';
import { environment } from '../../../../environments/environment';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import { createDeveloperSection } from '@/dev/dev.menu';
import { GATBASE_MODULES, SessionStore, meetsRequirement } from '@/gatbase/core';

const SECTION_TITLE_KEY = 'menu.title';

const SECTION_TITLE_ENTRY = '@title';

@Component({
  selector: 'p-menu',
  standalone: true,
  imports: [AppMenuitem, RouterModule, TranslocoModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './app.menu.html'
})
export class AppMenu implements OnInit, OnDestroy {
  private readonly transloco = inject(TranslocoService);
  private readonly destroy$ = new Subject<void>();
  private readonly sessionStore = inject(SessionStore);
  private readonly modules = inject(GATBASE_MODULES);
  private readonly navLabels = signal<Record<string, string>>({});

  private readonly translationsVersion = signal(0);

  readonly model = computed<MenuItem[]>(() => {
    this.translationsVersion();

    return this.buildModel();
  });

  ngOnInit(): void {
    this.watchNavLabels();
    this.transloco
      .selectTranslation()
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => {
        this.translationsVersion.update((version) => version + 1);
      });
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
  }

  private buildModel(): MenuItem[] {
    const model: MenuItem[] = [];

    model.push({
      id: 'main',
      label: this.transloco.translate('menu.main'),
      items: [{ label: this.transloco.translate('menu.home'), icon: 'menu-icon menu-icon-home', routerLink: ['/home'] }]
    });

    model.push(...this.buildGatbaseSections());

    if (environment.production === false) {
      const developer = createDeveloperSection(this.transloco.translate('menu.developer'));

      if (developer !== null) {
        model.push(developer);
      }
    }

    return model;
  }

  /** One section per Gatbase module with at least one entry the session meets; none while anonymous. */
  private buildGatbaseSections(): MenuItem[] {
    const session = this.sessionStore.session();

    if (session === null) {
      return [];
    }

    const sections: MenuItem[] = [];

    for (const module of this.modules) {
      const items: MenuItem[] = module.nav
        .filter((entry) => meetsRequirement(session, entry.requires))
        .map((entry) => ({
          id: `nav-${entry.id}`,
          label: this.navLabels()[`${module.id}/${entry.id}`] ?? entry.labelKey,
          icon: entry.icon,
          routerLink: [entry.path]
        }));

      if (items.length > 0) {
        sections.push({ id: `module-${module.id}`, label: this.navLabels()[`${module.id}/${SECTION_TITLE_ENTRY}`] ?? module.id, items });
      }
    }

    return sections;
  }

  /** Follows the language: each nav label is read through the module's own Transloco scope. */
  private watchNavLabels(): void {
    for (const module of this.modules) {
      // A module names its menu section with `menu.title` in its scope; without it the section shows the module id.
      this.transloco
        .selectTranslate(SECTION_TITLE_KEY, {}, module.i18n)
        .pipe(takeUntil(this.destroy$))
        .subscribe((title) => {
          const isMissing = title === SECTION_TITLE_KEY || title === '';

          if (!isMissing) {
            this.navLabels.update((labels) => ({ ...labels, [`${module.id}/${SECTION_TITLE_ENTRY}`]: title }));
          }
        });

      for (const entry of module.nav) {
        this.transloco
          .selectTranslate(entry.labelKey, {}, module.i18n)
          .pipe(takeUntil(this.destroy$))
          .subscribe((label) => {
            this.navLabels.update((labels) => ({ ...labels, [`${module.id}/${entry.id}`]: label }));
          });
      }
    }
  }
}
