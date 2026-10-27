import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

/**
 * Page-level title row, rendered outside the white content card so the
 * screen title reads as its own section rather than as part of the card.
 */
@Component({
  selector: 'p-page-header',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="flex items-center justify-between mb-8">
      <div class="flex items-center gap-3">
        <h2 class="font-semibold m-0!">{{ title }}</h2>
        <ng-content select="[titleAction]" />
      </div>
      <div class="flex items-center gap-2">
        <ng-content />
      </div>
    </div>
  `
})
export class PageHeaderComponent {
  @Input({ required: true }) title = '';
}
