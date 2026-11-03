import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { clickRowAction, confirmRowAction } from '../core/row-actions';

/** The parameters screen (`/parameters`). */
export class ParametersPage {
  constructor(private readonly page: Page) {}

  /** The row whose `data-key` is `key`. */
  row(key: string): Locator {
    const row = this.page
      .getByTestId('parameters-row')
      .and(this.page.locator(`[data-key="${key}"]`));
    return row;
  }

  /** Effective value cell of a row. */
  value(key: string): Locator {
    const cell = this.row(key).getByTestId('parameters-cell-value');
    return cell;
  }

  /** Source cell of a row; its `data-value` is `global` or `tenant`. */
  source(key: string): Locator {
    const cell = this.row(key).getByTestId('parameters-cell-source');
    return cell;
  }

  /** Opens the override dialog of `key` from whichever row action the template shows. */
  async openOverride(key: string): Promise<void> {
    await clickRowAction(this.row(key), 'parameter-set-override');
    await expect(this.page.getByTestId('parameter-override-key')).toBeVisible();
  }

  /** Enters `value` in the override dialog and saves; waits for the PUT to answer 200. */
  async setOverride(key: string, value: number): Promise<void> {
    await this.openOverride(key);
    const input = this.page
      .getByTestId('parameter-override-value')
      .locator('input');
    await expect(input).toBeVisible();
    await input.fill(String(value));
    // The number input commits its model on blur.
    await input.press('Tab');
    const answered = this.page.waitForResponse(
      (response) =>
        response.url().includes('/tenant-override') &&
        response.request().method() === 'PUT',
    );
    await this.page.getByTestId('parameter-override-save').click();
    const response = await answered;
    expect(response.status()).toBe(200);
  }

  /** Clears the tenant override: the row action asks in the shared dialog. */
  async clearOverride(key: string): Promise<void> {
    await confirmRowAction(
      this.page,
      this.row(key),
      'parameter-clear-override',
      'Quitar valor propio',
    );
  }
}
