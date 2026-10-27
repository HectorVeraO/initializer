import { Component, computed, inject, input } from '@angular/core';
import { ButtonModule } from '@openng/optimus-ui/button';
import { StyleClassModule } from '@openng/optimus-ui/styleclass';
import { AppConfigurator } from '../configurator/app.configurator';
import { LayoutService } from '../../service/layout.service';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'p-floating-configurator',
  imports: [CommonModule, ButtonModule, StyleClassModule, AppConfigurator],
  templateUrl: './app.floatingconfigurator.html'
})
export class AppFloatingConfigurator {
  LayoutService = inject(LayoutService);

  float = input<boolean>(true);

  isDarkTheme = computed(() => this.LayoutService.layoutConfig().darkTheme);

  toggleDarkMode() {
    this.LayoutService.layoutConfig.update((state) => ({ ...state, darkTheme: !state.darkTheme }));
  }
}
