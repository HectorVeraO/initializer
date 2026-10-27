import { ChangeDetectionStrategy, Component, EventEmitter, Output } from '@angular/core';
import { TranslocoModule } from '@jsverse/transloco';

/**
 * Standalone "← Volver" link for a page reached from a list, without a
 * title row alongside it (the page renders its own title separately).
 * `p-form-header` covers the case where the back link and the title share
 * one row; this is for pages whose title needs a different layout (an
 * image, a description line) right below the link.
 */
@Component({
  selector: 'p-back-link',
  standalone: true,
  imports: [TranslocoModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <a class="inline-flex items-center gap-2 text-sm font-medium text-primary no-underline cursor-pointer mb-4" tabindex="0" (click)="back.emit()" (keydown.enter)="back.emit()">
      <i class="pi pi-arrow-left"></i>
      {{ 'common.back' | transloco }}
    </a>
  `
})
export class BackLinkComponent {
  @Output() back = new EventEmitter<void>();
}
