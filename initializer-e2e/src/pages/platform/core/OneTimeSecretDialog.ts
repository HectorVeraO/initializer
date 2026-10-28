import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The one-time secret dialog shared by invitation links and API keys. */
export class OneTimeSecretDialog {
  readonly dialog: Locator;
  readonly value: Locator;
  readonly closeButton: Locator;

  constructor(page: Page) {
    this.dialog = page.getByTestId('one-time-secret-dialog');
    this.value = page.getByTestId('one-time-secret-value');
    this.closeButton = page.getByTestId('one-time-secret-close');
  }

  /** The secret shown, once the dialog is open. */
  async secret(): Promise<string> {
    await expect(this.value).toBeVisible();
    const secret = await this.value.inputValue();
    return secret;
  }

  async close(): Promise<void> {
    await this.closeButton.click();
    await expect(this.value).toHaveCount(0);
  }
}
