import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

/**
 * Page-level title row, rendered outside the white content card so the
 * screen title reads as its own section rather than as part of the card.
 * `subtitle` is the lead line under the title: one sentence saying what the
 * view is. Elements marked `titleAction` sit next to the title (a refresh button); the rest of the projected content
 * is right-aligned (primary actions). `titleTestId` and `subtitleTestId` put a
 * `data-testid` on the title and the subtitle for pages whose specs address them.
 */
@Component({
  selector: 'p-page-header',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="mb-6 flex flex-wrap items-center justify-between gap-4">
      <div class="flex min-w-0 flex-col gap-1">
        <div class="flex items-center gap-2">
          <h1 class="m-0 text-display-xs font-medium text-color wrap-anywhere" [attr.data-testid]="titleTestId || null">{{ title }}</h1>
          <ng-content select="[titleAction]" />
        </div>
        @if (subtitle) {
          <p class="m-0 text-sm text-muted-color" [attr.data-testid]="subtitleTestId || null">{{ subtitle }}</p>
        }
      </div>
      <div class="flex items-center gap-2">
        <ng-content />
      </div>
    </div>
  `
})
export class PageHeaderComponent {
  @Input({ required: true }) title = '';
  @Input() subtitle = '';
  @Input() titleTestId = '';
  @Input() subtitleTestId = '';
}
