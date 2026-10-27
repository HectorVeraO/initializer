import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The public invitation page (`/invitations/accept`). */
export class AcceptInvitationPage {
  readonly fullName: Locator;
  readonly password: Locator;
  readonly passwordConfirm: Locator;
  readonly createAccount: Locator;
  readonly signIn: Locator;
  readonly error: Locator;
  readonly invalid: Locator;

  constructor(private readonly page: Page) {
    this.fullName = page.getByTestId('accept-new-fullname');
    this.password = page.getByTestId('accept-new-password');
    this.passwordConfirm = page.getByTestId('accept-new-password-confirm');
    this.createAccount = page.getByTestId('accept-new-submit');
    this.signIn = page.getByTestId('accept-sign-in');
    this.error = page.getByTestId('accept-error');
    this.invalid = page.getByTestId('accept-invalid');
  }

  /** Fills the new-account form and submits it. */
  async createNewAccount(fullName: string, password: string): Promise<void> {
    await expect(this.fullName).toBeVisible();
    await this.fullName.fill(fullName);
    await this.password.fill(password);
    await this.passwordConfirm.fill(password);
    await this.createAccount.click();
  }

  /** Path and fragment of the current URL. */
  location(): { pathname: string; hash: string } {
    const url = new URL(this.page.url());
    return { pathname: url.pathname, hash: url.hash };
  }
}
