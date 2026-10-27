import { Component, OnDestroy, OnInit, computed, inject, ChangeDetectionStrategy, signal } from '@angular/core';
import { RouterModule } from '@angular/router';
import { MenuItem } from '@openng/optimus-ui/api';
import { AppMenuitem } from '../menu-item/app.menuitem';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';
import { environment } from '../../../../environments/environment';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import { createDeveloperSection } from '@/dev/dev.menu';

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

  private readonly translationsVersion = signal(0);

  readonly model = computed<MenuItem[]>(() => {
    this.translationsVersion();

    return this.buildModel();
  });

  ngOnInit(): void {
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

    if (environment.production === false) {
      const developer = createDeveloperSection(this.transloco.translate('menu.developer'));

      if (developer !== null) {
        model.push(developer);
      }
    }

    return model;
  }
}
