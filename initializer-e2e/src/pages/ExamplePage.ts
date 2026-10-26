import { Page, Locator, expect } from '@playwright/test';
import { loadEnv, buildBaseUrl } from '../env';

/**
 * Example Page Object — replace with real pages for your app.
 *
 * Selector priority: stable id > ARIA role/text > data-testid. Avoid fragile
 * positional / layout-coupled XPath or CSS.
 */
export class ExamplePage {
  readonly page: Page;
  readonly root: Locator;

  constructor(page: Page) {
    this.page = page;
    // TODO(hvera): point at a stable element of your app shell.
    this.root = page.locator('body');
  }

  async goto(): Promise<void> {
    const env = loadEnv();
    await this.page.goto(buildBaseUrl(env));
    await expect(this.root).toBeVisible();
  }
}
