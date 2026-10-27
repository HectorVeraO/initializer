import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

/**
 * Numbered section card for multi-step creation/edit forms (monitoring-form),
 * matching the high-fidelity wireframe's separated-card layout: each section
 * is its own elevated `.card` instead of a shared p-fieldset legend inside one
 * outer card. `stepNumber` is optional so the same card style can wrap an
 * un-numbered section. Spacing between instances is the caller's job (e.g. a
 * `flex flex-col gap-*` wrapper) — `.card`'s own `margin-bottom`/`:last-child`
 * rule always resolves to 0 here, since this div is always the sole child of
 * its own host element.
 */
@Component({
  selector: 'p-form-section',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="card">
      <div class="flex items-center gap-2 mb-4">
        @if (stepNumber) {
          <span class="flex items-center justify-center w-6 h-6 rounded-full bg-primary-500 text-white text-xs font-semibold shrink-0">{{ stepNumber }}</span>
        }
        <h3 class="font-semibold m-0!">{{ title }}</h3>
      </div>
      <ng-content />
    </div>
  `
})
export class FormSectionComponent {
  @Input() stepNumber?: number;
  @Input({ required: true }) title = '';
}
