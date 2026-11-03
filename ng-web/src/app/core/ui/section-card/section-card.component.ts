import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { PanelModule } from '@openng/optimus-ui/panel';

export type SectionCardTone = 'neutral' | 'primary' | 'success' | 'danger';

const ICON_TILE_CLASSES: Record<SectionCardTone, string> = {
  neutral: 'bg-surface-100 text-surface-700 dark:bg-surface-800 dark:text-surface-200',
  primary: 'bg-highlight',
  success: 'bg-success-50 text-success-700 dark:bg-success-500/15 dark:text-success-400',
  danger: 'bg-danger-50 text-danger-700 dark:bg-danger-500/15 dark:text-danger-400'
};

const FLUSH_TOKENS = { content: { padding: '0' } };

/** The panel body is a grid item; without `min-w-0` a wide table stretches the page instead of scrolling inside its own container. */
const PASS_THROUGH = { contentWrapper: { class: 'min-w-0' } };

/**
 * Card with a header row (optional tinted icon tile, title, subtitle and a
 * right-aligned actions slot), a divider and a body. Built on `p-panel`.
 * Mark header elements with `cardActions` (badges, buttons) or `cardSubtitle`
 * (a rich subtitle line under the title); everything else is the body. `flush` removes the body padding (a table edge to edge).
 */
@Component({
  selector: 'p-section-card',
  standalone: true,
  imports: [PanelModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block min-w-0' },
  template: `
    <p-panel [dt]="flush() ? flushTokens : undefined" [pt]="passThrough">
      <ng-template #header>
        <div class="flex w-full items-start gap-3">
          @if (icon()) {
            <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md" [class]="iconTileClass()" [attr.data-tone]="iconTone()">
              <i [class]="icon()" aria-hidden="true"></i>
            </span>
          }
          <div class="flex min-w-0 flex-1 flex-col gap-0.5">
            <h2 class="m-0 text-lg font-medium text-color">{{ title() }}</h2>
            @if (subtitle()) {
              <p class="m-0 text-xs text-muted-color">{{ subtitle() }}</p>
            }
            <ng-content select="[cardSubtitle]" />
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <ng-content select="[cardActions]" />
          </div>
        </div>
      </ng-template>
      <ng-content />
    </p-panel>
  `
})
export class SectionCardComponent {
  readonly title = input.required<string>();
  readonly subtitle = input('');
  readonly icon = input('');
  readonly iconTone = input<SectionCardTone>('neutral');
  readonly flush = input(false);

  protected readonly flushTokens = FLUSH_TOKENS;
  protected readonly passThrough = PASS_THROUGH;

  protected readonly iconTileClass = computed(() => ICON_TILE_CLASSES[this.iconTone()]);
}
