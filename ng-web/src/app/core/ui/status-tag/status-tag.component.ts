import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';
import { TagModule } from '@openng/optimus-ui/tag';
import { merge } from 'rxjs';
import { STATUS_TABLE, StatusFamily, resolveStatus } from './status-severity';

/** Horizontal padding of a tag, added to the width of its longest label. */
const TAG_PADDING = '1.25rem';

/**
 * Tag for one value of a closed set (review status, decision, item type,
 * classification, outcome, event type, active flag). Severity and label come
 * from `STATUS_TABLE`, so a value looks the same on every page. The tag carries
 * `data-value` (the raw value) and `data-severity` for tests. A null value
 * renders an em dash; an unknown value renders its raw text on a neutral tag.
 * Every tag of a family has the width of that family's longest label (in the
 * active language) with its text centered, so tags in a column line up and the
 * text next to them starts at the same place on every row.
 * Put `data-testid` on the host element.
 */
@Component({
  selector: 'p-status-tag',
  standalone: true,
  imports: [TagModule, TranslocoModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'inline-flex' },
  template: `
    @if (view(); as current) {
      <p-tag [severity]="current.severity" [style]="{ 'min-width': minWidth(), 'justify-content': 'center' }" [attr.data-value]="current.raw" [attr.data-severity]="current.severity">
        @if (current.labelKey) {
          {{ current.labelKey | transloco }}
        } @else {
          {{ current.raw }}
        }
      </p-tag>
    } @else {
      <span class="text-muted-color">&mdash;</span>
    }
  `
})
export class StatusTagComponent {
  readonly family = input.required<StatusFamily>();
  readonly value = input.required<string | boolean | null | undefined>();

  private readonly transloco = inject(TranslocoService);

  // Emits when the language changes or a translation file finishes loading, so the width follows the labels.
  private readonly translationTick = toSignal(merge(this.transloco.langChanges$, this.transloco.events$), { initialValue: null });

  protected readonly view = computed(() => resolveStatus(this.family(), this.value()));

  protected readonly minWidth = computed(() => {
    this.translationTick();

    const labels = Object.values(STATUS_TABLE[this.family()]).map((entry) => this.transloco.translate(entry.labelKey));

    const longest = labels.reduce((max, label) => Math.max(max, label.length), 0);

    return `calc(${longest}ch + ${TAG_PADDING})`;
  });
}
