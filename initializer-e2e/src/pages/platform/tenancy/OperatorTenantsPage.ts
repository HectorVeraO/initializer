import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The operator tenants screen (`/operator/tenants`). */
export class OperatorTenantsPage {
  readonly slug: Locator;
  readonly name: Locator;
  readonly adminEmail: Locator;
  readonly submit: Locator;

  constructor(private readonly page: Page) {
    this.slug = page.getByTestId('tenant-slug');
    this.name = page.getByTestId('tenant-name');
    this.adminEmail = page.getByTestId('tenant-admin-email');
    this.submit = page.getByTestId('tenant-submit');
  }

  /** Fills the create form and submits it. */
  async create(slug: string, name: string, adminEmail: string): Promise<void> {
    await expect(this.slug).toBeVisible();
    await this.slug.fill(slug);
    await this.name.fill(name);
    await this.adminEmail.fill(adminEmail);
    await this.submit.click();
  }

  /** The row of the tenant with this slug. */
  row(slug: string): Locator {
    const row = this.page
      .getByTestId('tenants-row')
      .and(this.page.locator(`[data-key="${slug}"]`));
    return row;
  }

  /** Status cell of a row. */
  status(slug: string): Locator {
    const cell = this.row(slug).getByTestId('tenants-cell-status');
    return cell;
  }

  /** Suspend with the inline confirmation. */
  async suspend(slug: string): Promise<void> {
    const row = this.row(slug);
    await row.getByTestId('tenant-suspend').click();
    await row.getByTestId('tenant-suspend-confirm').click();
  }

  async activate(slug: string): Promise<void> {
    await this.row(slug).getByTestId('tenant-activate').click();
  }
}
