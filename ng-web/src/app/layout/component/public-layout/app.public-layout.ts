import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';

/** Frame of the pages outside the shell (login, invitation acceptance): the page fills the viewport. */
@Component({
  selector: 'p-public-layout',
  standalone: true,
  imports: [RouterOutlet],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: ` <router-outlet /> `
})
export class AppPublicLayout {}
