import { Component, computed, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { NgComponentOutlet } from '@angular/common';
import { RouterModule } from '@angular/router';
import { SelectModule } from '@openng/optimus-ui/select';
import { FormsModule } from '@angular/forms';
import { PLATFORM_MODULES, platformTopbar } from '@/platform/core';
import { BRAND } from '@/core/config/brand';
import { LayoutService } from '../../service/layout.service';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';

@Component({
  selector: 'p-topbar',
  standalone: true,
  imports: [RouterModule, TranslocoModule, SelectModule, FormsModule, NgComponentOutlet],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './app.topbar.html'
})
export class AppTopbar {
  protected readonly brand = BRAND;

  public readonly layoutService = inject(LayoutService);
  private readonly translocoService = inject(TranslocoService);

  private readonly modules = inject(PLATFORM_MODULES);

  protected readonly contextItems = platformTopbar(this.modules, 'context');
  protected readonly accountItems = platformTopbar(this.modules, 'account');

  protected readonly languages = [{ code: 'es' }, { code: 'en' }];

  protected readonly activeLang = signal(this.translocoService.getActiveLang());
  protected readonly selectedLang = computed(() => this.languages.find((l) => l.code === this.activeLang()) ?? this.languages[0]);

  setLang(code: string): void {
    this.translocoService.setActiveLang(code);
    this.activeLang.set(code);
  }

  toggleDarkMode(): void {
    this.layoutService.layoutConfig.update((state) => ({ ...state, darkTheme: !state.darkTheme }));
  }
}
