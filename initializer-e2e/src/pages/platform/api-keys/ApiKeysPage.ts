import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The admin API keys screen (`/api-keys`). */
export class ApiKeysPage {
  readonly name: Locator;
  readonly submit: Locator;
  readonly owner: Locator;
  readonly scopesError: Locator;
  readonly rows: Locator;
  readonly secretDialog: Locator;
  readonly secretValue: Locator;

  constructor(private readonly page: Page) {
    this.name = page.getByTestId('api-key-name');
    this.submit = page.getByTestId('api-key-submit');
    this.owner = page.getByTestId('api-keys-owner');
    this.scopesError = page.getByTestId('api-key-scopes-error');
    this.rows = page.getByTestId('api-keys-row');
    this.secretDialog = page.getByTestId('one-time-secret-dialog');
    this.secretValue = page.getByTestId('one-time-secret-value');
  }

  /** Fills the create form with one scope and submits it. */
  async create(name: string, scope: string): Promise<void> {
    await expect(this.name).toBeVisible();
    await this.name.fill(name);
    const box = this.page
      .getByTestId('api-key-scope')
      .and(this.page.locator(`[data-key="${scope}"]`))
      .locator('input');
    await box.check();
    await this.submit.click();
  }

  /** The row whose `data-key` is `name`. */
  row(name: string): Locator {
    const row = this.page
      .getByTestId('api-keys-row')
      .and(this.page.locator(`[data-key="${name}"]`));
    return row;
  }

  /** Prefix cell of a row. */
  prefix(name: string): Locator {
    const cell = this.row(name).getByTestId('api-keys-cell-prefix');
    return cell;
  }

  /** Status cell of a row. */
  status(name: string): Locator {
    const cell = this.row(name).getByTestId('api-keys-cell-status');
    return cell;
  }

  /** Revoke with the inline confirmation. */
  async revoke(name: string): Promise<void> {
    const row = this.row(name);
    await row.getByTestId('api-key-revoke').click();
    await row.getByTestId('api-key-revoke-confirm').click();
  }
}
