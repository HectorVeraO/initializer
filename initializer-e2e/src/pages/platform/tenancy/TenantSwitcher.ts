import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/** The topbar tenant switcher of the authenticated console. */
export class TenantSwitcher {
  constructor(private readonly page: Page) {}

  /** Slug shown in the topbar, or null without an active tenant. */
  async activeTenantSlug(): Promise<string | null> {
    const label = this.page.getByTestId('active-tenant-slug');
    if ((await label.count()) === 0) {
      return null;
    }
    const slug = (await label.textContent())?.trim() ?? '';
    return slug === '' ? null : slug;
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
}
