import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TranslocoModule } from '@jsverse/transloco';

@Component({
  selector: 'p-forbidden',
  standalone: true,
  imports: [RouterLink, TranslocoModule],
  template: `
    <div class="card" data-testid="forbidden">
      <h1 class="text-2xl font-semibold mb-2">{{ 'forbidden.title' | transloco }}</h1>
      <a routerLink="/" class="text-primary">{{ 'forbidden.back' | transloco }}</a>
    </div>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class ForbiddenComponent {}
