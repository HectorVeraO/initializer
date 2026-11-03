import { inject } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';
import { ConfirmationService } from '@openng/optimus-ui/api';

/** Labels of a destructive confirmation; the two optional ones fall back to the console defaults. */
export interface DestructiveConfirmation {
  /** Text of the dialog; defaults to `confirms.defaultMessage`. */
  readonly message?: string;
  /** Label of the accept button: the action itself ("Revocar", "Suspender"). */
  readonly acceptLabel: string;
  /** Label of the reject button; defaults to `common.cancel`. */
  readonly rejectLabel?: string;
}

/** `confirmDestructive` bound to the shared confirmation dialog of the console. */
export type ConfirmDestructive = (confirmation: DestructiveConfirmation, accept: () => void) => void;

/**
 * The one-click confirmation of a destructive row action: it resolves the same
 * message and cancel label on every screen, so a page passes only the action's
 * label and what to run, and the person answers with a single click. Deleting a
 * named record asks for the typed keyword instead (`ConfirmDeleteService`).
 */
export function injectConfirmDestructive(): ConfirmDestructive {
  const confirmation = inject(ConfirmationService);

  const transloco = inject(TranslocoService);

  return (request, accept) => {
    confirmation.confirm({
      message: request.message ?? transloco.translate('confirms.defaultMessage'),
      acceptLabel: request.acceptLabel,
      rejectLabel: request.rejectLabel ?? transloco.translate('common.cancel'),
      accept
    });
  };
}
