import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { clickRowAction, confirmRowAction } from '../core/row-actions';

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

  /** Opens the restyled create dialog; the previous screen renders the form inline. */
  async openCreate(): Promise<void> {
    const headerAction = this.page.getByTestId('tenant-new');
    await expect(
      headerAction.or(this.page.getByTestId('tenant-form')),
    ).toBeVisible();
    if (await headerAction.isVisible()) {
      await headerAction.click();
    }
  }

  /**
   * Fills the create form and submits it, whichever template the screen shows.
   * A created tenant always ends as a row and, with the first-admin link, in
   * the one-time secret dialog.
   */
  async create(slug: string, name: string, adminEmail: string): Promise<void> {
    await this.openCreate();
    await expect(this.slug).toBeVisible();
    await this.slug.fill(slug);
    await this.name.fill(name);
    await this.adminEmail.fill(adminEmail);
    await this.submit.click();
    await expect(this.row(slug)).toBeVisible();
    await expect(this.page.getByTestId('one-time-secret-value')).toBeVisible();
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

  /** Suspends the tenant: the restyled row asks in the shared dialog, the previous one inline. */
  async suspend(slug: string): Promise<void> {
    await confirmRowAction(
      this.page,
      this.row(slug),
      'tenant-suspend',
      'Suspender',
    );
  }

  async activate(slug: string): Promise<void> {
    await clickRowAction(this.row(slug), 'tenant-activate');
  }
}
