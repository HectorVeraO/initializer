import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import type { SeedUser } from '../../../helpers/platform/api';

/** The console login page (`/login`). Selectors are data-testid only. */
export class LoginPage {
  readonly email: Locator;
  readonly password: Locator;
  readonly submit: Locator;
  readonly error: Locator;

  constructor(private readonly page: Page) {
    this.email = page.getByTestId('login-email');
    this.password = page.getByTestId('login-password');
    this.submit = page.getByTestId('login-submit');
    this.error = page.getByTestId('login-error');
  }

  /** Fills the form with `user`, submits it and waits for the URL path `targetPath`. */
  async signIn(user: SeedUser, targetPath: string): Promise<void> {
    await expect(this.email).toBeVisible();
    await this.email.fill(user.email);
    await this.password.fill(user.password);
    await this.submit.click();
    await expect.poll(() => new URL(this.page.url()).pathname).toBe(targetPath);
  }
}
