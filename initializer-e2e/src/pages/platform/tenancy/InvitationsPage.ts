import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The admin invitations screen (`/invitations`). */
export class InvitationsPage {
  readonly email: Locator;
  readonly submit: Locator;

  constructor(private readonly page: Page) {
    this.email = page.getByTestId('invite-email');
    this.submit = page.getByTestId('invite-submit');
  }

  /** Fills the invite form (role left on its default `member` unless given) and submits it. */
  async invite(
    email: string,
    role?: 'admin' | 'member' | 'viewer',
  ): Promise<void> {
    await expect(this.email).toBeVisible();
    await this.email.fill(email);
    if (role !== undefined) {
      await this.page.getByTestId('invite-role').click();
      await this.page
        .getByTestId('invite-role-option')
        .and(this.page.locator(`[data-key="${role}"]`))
        .click();
    }
    await this.submit.click();
  }

  /** The row whose `data-key` starts with `emailPrefix` (the part before the `@`). */
  row(emailPrefix: string): Locator {
    const row = this.page
      .getByTestId('invitations-row')
      .and(this.page.locator(`[data-key^="${emailPrefix}@"]`));
    return row;
  }

  /** Status cell of a row. */
  status(emailPrefix: string): Locator {
    const cell = this.row(emailPrefix).getByTestId('invitations-cell-status');
    return cell;
  }
}
