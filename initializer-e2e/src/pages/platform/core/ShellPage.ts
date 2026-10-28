import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The authenticated console shell: topbar tenant switcher, account menu and side nav. */
export class ShellPage {
  readonly accountEmailLocator: Locator;
  readonly operatorBadge: Locator;
  readonly signOutButton: Locator;

  constructor(private readonly page: Page) {
    this.accountEmailLocator = page.getByTestId('session-account-email');
    this.operatorBadge = page.getByTestId('session-operator');
    this.signOutButton = page.getByTestId('sign-out');
  }

  /** Slug shown in the topbar, or null without an active tenant. */
  async activeTenantSlug(): Promise<string | null> {
    const label = this.page.getByTestId('active-tenant-slug');
    if ((await label.count()) === 0) {
      return null;
    }
    const slug = (await label.textContent())?.trim() ?? '';
    return slug === '' ? null : slug;
  }

  /** Email of the signed-in account, once the topbar shows it. */
  async accountEmail(): Promise<string> {
    await expect(this.accountEmailLocator).toBeVisible();
    const email = (await this.accountEmailLocator.textContent())?.trim() ?? '';
    return email;
  }

  /** Opens the tenant switcher, picks the tenant and waits for the switch to answer 200. */
  async switchTo(slug: string): Promise<void> {
    await this.page.getByTestId('tenant-switcher').click();
    const answered = this.page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/auth/switch-tenant') &&
        response.request().method() === 'POST',
    );
    await this.page
      .getByTestId('tenant-option')
      .and(this.page.locator(`[data-key="${slug}"]`))
      .click();
    const response = await answered;
    expect(response.status()).toBe(200);
  }

  /** Clicks the side-nav entry `nav-<id>`. */
  async nav(id: string): Promise<void> {
    await this.page.getByTestId(`nav-${id}`).click();
  }

  async signOut(): Promise<void> {
    await this.signOutButton.click();
  }
}
