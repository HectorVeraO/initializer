import { Component, computed, inject, input, signal, ChangeDetectionStrategy } from '@angular/core';
import { TranslocoModule } from '@jsverse/transloco';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';

/** Full-page error. With no `errorKey` it explains why `runtime-config.json` could not be used. */
@Component({
  selector: 'p-error-page',
  imports: [TranslocoModule],
  templateUrl: './error-page.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrl: './error-page.component.scss'
})
export class ErrorPageComponent {
  private readonly runtimeConfig = inject(RuntimeConfigService);

  errorKey = input<string>('config');
  isRetrying = signal(false);

  readonly missingFields = computed(() => {
    const failure = this.runtimeConfig.failure();

    return failure?.kind === 'invalid' ? failure.missingFields : [];
  });

  onRetry(): void {
    if (this.isRetrying()) {
      return;
    }

    this.isRetrying.set(true);
    window.location.assign(window.location.origin);
  }
}
