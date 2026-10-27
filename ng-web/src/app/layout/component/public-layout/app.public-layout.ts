import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { AppFloatingConfigurator } from '../floating-configurator/app.floatingconfigurator';

/** Frame of the pages outside the shell (login): the page plus the floating theme configurator. */
@Component({
  selector: 'p-public-layout',
  standalone: true,
  imports: [RouterOutlet, AppFloatingConfigurator],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <p-floating-configurator />
    <router-outlet />
  `
})
export class AppPublicLayout {}
