import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { Router, RouterLink } from '@angular/router';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';
import { MenuItem } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { MenuModule } from '@openng/optimus-ui/menu';
import { TooltipModule } from '@openng/optimus-ui/tooltip';
import { merge } from 'rxjs';

/** Where a navigation action goes; rendered as a real link when shown inline. */
export interface RowActionLink {
  commands: readonly unknown[];
  queryParams?: Record<string, unknown>;
}

/** One action of a table row. Give `labelKey` (translated here) or an already translated `label`. */
export interface RowAction {
  /** Unique within the row. */
  id: string;
  /** Already translated text. Ignored when `labelKey` is set. */
  label?: string;
  /** Translation key of the text; follows the active language. */
  labelKey?: string;
  /** Icon classes, e.g. `pi pi-pencil`. */
  icon: string;
  /** Rendered as `data-testid` on the button, link or menu item so specs click the same logical element. */
  testId?: string;
  /** Draws the action in the danger color (delete and similar). */
  destructive?: boolean;
  /** Permission-gated or state-gated action: it stays shown but cannot be activated. */
  disabled?: boolean;
  /** Removes the action from the row and from the count that decides between buttons and the menu. */
  hidden?: boolean;
  /** Navigation target; an action has either `link` or `run`. */
  link?: RowActionLink;
  run?: () => void;
}

/** From this many visible actions on, the row shows one three-dots menu instead of icon buttons. */
export const ROW_ACTIONS_MENU_THRESHOLD = 3;

interface ResolvedAction extends RowAction {
  text: string;
}

/**
 * Actions of one table row. Icon-only buttons with a tooltip (the tooltip text
 * is also the accessible name) while the row has fewer than three visible
 * actions; a single vertical three-dots button that opens a popup menu with
 * every action (icon and text) from three on. Navigation actions render as
 * links so they keep middle-click and open-in-new-tab. The menu trigger is a
 * native button (Enter, Space and Escape work); the popup is the library menu
 * (arrow keys, Escape, focus return).
 */
@Component({
  selector: 'p-row-actions',
  standalone: true,
  imports: [TranslocoModule, ButtonModule, MenuModule, TooltipModule, RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'inline-flex items-center justify-end gap-1' },
  template: `
    @if (useMenu()) {
      <button
        pButton
        type="button"
        severity="secondary"
        [text]="true"
        size="small"
        aria-haspopup="menu"
        data-testid="row-actions-trigger"
        [pTooltip]="triggerLabel()"
        tooltipPosition="top"
        [attr.aria-label]="triggerLabel()"
        (click)="menu.toggle($event)"
      >
        <i pButtonIcon class="pi pi-ellipsis-v"></i>
      </button>
      <p-menu #menu [popup]="true" appendTo="body" [model]="items()">
        <ng-template #item let-item>
          <span class="flex items-center gap-2 px-3 py-2 text-sm" [class]="isDestructive(item) ? 'text-danger-600 dark:text-danger-400' : 'text-color'" [attr.data-testid]="testIdOf(item)">
            <i [class]="item.icon" aria-hidden="true"></i>
            <span>{{ item.label }}</span>
          </span>
        </ng-template>
      </p-menu>
    } @else {
      @for (action of visible(); track action.id) {
        @if (action.link && action.disabled !== true) {
          <a
            pButton
            [severity]="action.destructive ? 'danger' : 'secondary'"
            [text]="true"
            size="small"
            [routerLink]="action.link.commands"
            [queryParams]="action.link.queryParams"
            [pTooltip]="action.text"
            tooltipPosition="top"
            [attr.aria-label]="action.text"
            [attr.data-testid]="action.testId ?? null"
          >
            <i pButtonIcon [class]="action.icon"></i>
          </a>
        } @else {
          <button
            pButton
            type="button"
            [severity]="action.destructive ? 'danger' : 'secondary'"
            [text]="true"
            size="small"
            [disabled]="action.disabled === true"
            [pTooltip]="action.text"
            tooltipPosition="top"
            [attr.aria-label]="action.text"
            [attr.data-testid]="action.testId ?? null"
            (click)="runAction(action)"
          >
            <i pButtonIcon [class]="action.icon"></i>
          </button>
        }
      }
    }
  `
})
export class RowActionsComponent {
  readonly actions = input.required<RowAction[]>();
  /** Accessible name and tooltip of the three-dots trigger; defaults to the translated generic label. */
  readonly ariaLabel = input('');

  private readonly transloco = inject(TranslocoService);

  private readonly router = inject(Router);

  // Emits when the language changes or a translation file finishes loading, so texts resolved here stay current.
  private readonly translationTick = toSignal(merge(this.transloco.langChanges$, this.transloco.events$), { initialValue: null });

  protected readonly visible = computed<ResolvedAction[]>(() => {
    this.translationTick();

    return this.actions()
      .filter((action) => action.hidden !== true)
      .map((action) => ({ ...action, text: this.textOf(action) }));
  });

  protected readonly useMenu = computed(() => this.visible().length >= ROW_ACTIONS_MENU_THRESHOLD);

  protected readonly triggerLabel = computed(() => {
    this.translationTick();

    return this.ariaLabel() || this.transloco.translate('common.rowActions');
  });

  protected readonly items = computed<MenuItem[]>(() =>
    this.visible().map((action) => ({
      id: action.id,
      label: action.text,
      icon: action.icon,
      disabled: action.disabled === true,
      command: () => this.runAction(action)
    }))
  );

  protected runAction(action: RowAction): void {
    if (action.link !== undefined) {
      void this.router.navigate([...action.link.commands], { queryParams: action.link.queryParams });

      return;
    }

    action.run?.();
  }

  protected isDestructive(item: MenuItem): boolean {
    return this.findAction(item)?.destructive === true;
  }

  protected testIdOf(item: MenuItem): string | null {
    return this.findAction(item)?.testId ?? null;
  }

  private textOf(action: RowAction): string {
    if (action.labelKey !== undefined) {
      return this.transloco.translate(action.labelKey);
    }

    return action.label ?? '';
  }

  private findAction(item: MenuItem): RowAction | undefined {
    return this.actions().find((action) => action.id === item.id);
  }
}
