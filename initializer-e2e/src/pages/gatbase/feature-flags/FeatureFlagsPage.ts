import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The feature-flags screens (`/operator/feature-flags` and `/operator/feature-flags/<key>`). */
export class FeatureFlagsPage {
  constructor(private readonly page: Page) {}

  /** The list row whose `data-key` is `key`. */
  row(key: string): Locator {
    const row = this.page
      .getByTestId('flags-row')
      .and(this.page.locator(`[data-key="${key}"]`));
    return row;
  }

  /** Type cell of a list row; its `data-value` is `ops` or `release`. */
  type(key: string): Locator {
    const cell = this.row(key).getByTestId('flags-cell-type');
    return cell;
  }

  /** Expiry cell of a list row. */
  expires(key: string): Locator {
    const cell = this.row(key).getByTestId('flags-cell-expires');
    return cell;
  }

  /** Opens the detail of a flag from the list. */
  async open(key: string): Promise<void> {
    await this.row(key).getByTestId('flag-open').click();
    await expect(this.page.getByTestId('flag-tenant-form')).toBeVisible();
  }

  /** The tenant override row whose `data-key` is the tenant slug. */
  tenantRow(slug: string): Locator {
    const row = this.page
      .getByTestId('flag-tenant-row')
      .and(this.page.locator(`[data-key="${slug}"]`));
    return row;
  }

  /** Enabled cell of a tenant override row; its `data-value` is `true` or `false`. */
  tenantEnabled(slug: string): Locator {
    const cell = this.tenantRow(slug).getByTestId('flag-tenant-cell-enabled');
    return cell;
  }

  /** Picks the tenant `slug`, sets the toggle to `enabled` and saves; waits for the PUT to answer 200. */
  async addTenantOverride(slug: string, enabled: boolean): Promise<void> {
    await this.page.getByTestId('flag-tenant-select').click();
    await this.page
      .getByTestId('flag-tenant-option')
      .and(this.page.locator(`[data-key="${slug}"]`))
      .click();
    const toggle = this.page
      .getByTestId('flag-tenant-enabled')
      .getByRole('switch');
    if ((await toggle.isChecked()) !== enabled) {
      await toggle.click();
    }
    const answered = this.page.waitForResponse(
      (response) =>
        response.url().includes('/tenants/') &&
        response.request().method() === 'PUT',
    );
    await this.page.getByTestId('flag-tenant-save').click();
    const response = await answered;
    expect(response.status()).toBe(200);
  }

  /** Removes the override of a tenant with the inline confirmation. */
  async removeTenantOverride(slug: string): Promise<void> {
    const row = this.tenantRow(slug);
    await row.getByTestId('flag-tenant-remove').click();
    await row.getByTestId('flag-tenant-remove-confirm').click();
  }
}
