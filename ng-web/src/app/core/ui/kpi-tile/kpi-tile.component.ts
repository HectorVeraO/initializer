import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { SkeletonModule } from '@openng/optimus-ui/skeleton';

export type KpiTone = 'neutral' | 'success' | 'danger';

export type KpiSize = 'lg' | 'md';

const VALUE_CLASSES: Record<KpiTone, string> = {
  neutral: 'text-color',
  success: 'text-success-600 dark:text-success-400',
  danger: 'text-danger-600 dark:text-danger-400'
};

const SIZE_CLASSES: Record<KpiSize, string> = {
  lg: 'text-display-sm font-medium',
  md: 'text-lg font-medium wrap-anywhere line-clamp-2'
};

/**
 * Bordered indicator tile: a label, a large value and an optional caption or
 * link projected below it. `tone` colors the value (green for a good figure,
 * red for a failure count). A null value renders as an em dash. `size="md"`
 * fits a short text value (a date, a version) instead of a number; `loading`
 * swaps the value for a skeleton; `valueTestId` tags the value element.
 */
@Component({
  selector: 'p-kpi-tile',
  standalone: true,
  imports: [SkeletonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="flex h-full flex-col gap-2 rounded-lg border border-surface bg-surface-0 px-4 py-5 dark:bg-surface-800" [attr.data-tone]="tone()" [attr.aria-busy]="loading() || null">
      <span class="text-sm text-color">{{ label() }}</span>
      @if (loading()) {
        <p-skeleton width="6rem" [height]="size() === 'lg' ? '2.375rem' : '1.75rem'" />
      } @else {
        <span [class]="valueClass()" [attr.title]="size() === 'md' ? displayValue() : null" [attr.data-testid]="valueTestId() || null">{{ displayValue() }}</span>
      }
      <ng-content />
    </div>
  `
})
export class KpiTileComponent {
  readonly label = input.required<string>();
  readonly value = input<string | number | null>(null);
  readonly tone = input<KpiTone>('neutral');
  readonly size = input<KpiSize>('lg');
  readonly loading = input(false);
  readonly valueTestId = input('');

  protected readonly valueClass = computed(() => `${SIZE_CLASSES[this.size()]} ${VALUE_CLASSES[this.tone()]}`);

  protected readonly displayValue = computed(() => {
    const value = this.value();

    return value === null ? '—' : String(value);
  });
}
