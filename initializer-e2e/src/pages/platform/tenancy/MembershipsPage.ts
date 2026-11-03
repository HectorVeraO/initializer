import type { Locator, Page } from '@playwright/test';
import { clickRowAction, confirmRowAction } from '../core/row-actions';

/** The members screen (`/memberships`). */
export class MembershipsPage {
  constructor(private readonly page: Page) {}

  /** The row whose `data-key` starts with `emailPrefix` (the part before the `@`). */
  row(emailPrefix: string): Locator {
    const row = this.page
      .getByTestId('memberships-row')
      .and(this.page.locator(`[data-key^="${emailPrefix}@"]`));
    return row;
  }

  /** Role cell of a row. */
  role(emailPrefix: string): Locator {
    const cell = this.row(emailPrefix).getByTestId('memberships-cell-role');
    return cell;
  }

  /** Status cell of a row. */
  status(emailPrefix: string): Locator {
    const cell = this.row(emailPrefix).getByTestId('memberships-cell-status');
    return cell;
  }

  /** Picks `role` in the row's select and waits for the PATCH to answer 200. */
  async changeRole(
    emailPrefix: string,
    role: 'admin' | 'member' | 'viewer',
  ): Promise<void> {
    await this.row(emailPrefix).getByTestId('membership-role-select').click();
    const answered = this.page.waitForResponse(
      (response) =>
        response.url().includes('/api/v1/memberships/') &&
        response.request().method() === 'PATCH' &&
        response.status() === 200,
    );
    await this.page
      .getByTestId('membership-role-option')
      .and(this.page.locator(`[data-key="${role}"]`))
      .click();
    await answered;
  }

  /** Suspends the member: the row action asks in the shared dialog. */
  async suspend(emailPrefix: string): Promise<void> {
    await confirmRowAction(
      this.page,
      this.row(emailPrefix),
      'membership-suspend',
      'Suspender',
    );
  }

  async reactivate(emailPrefix: string): Promise<void> {
    await clickRowAction(this.row(emailPrefix), 'membership-reactivate');
  }
}
