import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TranslocoModule } from '@jsverse/transloco';
import { MessageModule } from '@openng/optimus-ui/message';
import { ProgressSpinnerModule } from '@openng/optimus-ui/progressspinner';

/**
 * Common states of a screen backed by one read: loading, empty, load error
 * with "Reintentar" and not found. The projected content shows only when none
 * of them applies. Test ids are `<feature>-loading|empty|load-error|not-found`.
 */
@Component({
  selector: 'p-list-state',
  standalone: true,
  imports: [TranslocoModule, MessageModule, ProgressSpinnerModule, RouterLink],
  templateUrl: './list-state.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class ListStateComponent {
  readonly feature = input.required<string>();
  readonly loading = input(false);
  readonly loadFailed = input(false);
  readonly empty = input(false);
  readonly notFound = input(false);
  readonly sectionKey = input('');
  readonly emptyKey = input('');
  readonly notFoundKey = input('');
  readonly backPath = input('');

  readonly retry = output<void>();
}
