import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TranslocoModule } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';

@Component({
  selector: 'p-forbidden',
  standalone: true,
  imports: [RouterLink, TranslocoModule, ButtonModule],
  template: `
    <div class="flex min-h-[60vh] items-center justify-center">
      <div class="flex w-full max-w-md flex-col items-center gap-4 rounded-xl border border-surface bg-surface-0 p-10 text-center dark:bg-surface-900" data-testid="forbidden">
        <span class="flex h-12 w-12 items-center justify-center rounded-lg bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300">
          <i class="pi pi-lock text-xl" aria-hidden="true"></i>
        </span>
        <h1 class="m-0 text-display-xs font-medium text-color">{{ 'forbidden.title' | transloco }}</h1>
        <p class="m-0 text-md text-muted-color">{{ 'forbidden.message' | transloco }}</p>
        <p-button [label]="'forbidden.back' | transloco" routerLink="/" />
      </div>
    </div>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class ForbiddenComponent {}
