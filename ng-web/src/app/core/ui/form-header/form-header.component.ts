import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { TranslocoModule } from '@jsverse/transloco';

/**
 * "← Back" link plus page title, rendered outside the white content card,
 * for create/edit forms reached from a list. Mirrors p-page-header's outside-the-card placement and
 * horizontal padding, but adds the back-navigation link forms need instead of
 * the action-button slot list pages need.
 */
@Component({
  selector: 'p-form-header',
  standalone: true,
  imports: [TranslocoModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <a class="inline-flex items-center gap-2 text-sm font-medium text-primary no-underline cursor-pointer mb-2" [class.px-8]="!noPadding" tabindex="0" (click)="back.emit()" (keydown.enter)="back.emit()">
      <i class="pi pi-arrow-left"></i>
      {{ 'common.back' | transloco }}
    </a>

    <div class="flex items-center justify-between mb-8" [class.px-8]="!noPadding">
      <h2 class="font-semibold m-0!">{{ title }}</h2>
      <ng-content />
    </div>
  `
})
export class FormHeaderComponent {
  @Input({ required: true }) title = '';
  @Input() noPadding = false;
  @Output() back = new EventEmitter<void>();
}
