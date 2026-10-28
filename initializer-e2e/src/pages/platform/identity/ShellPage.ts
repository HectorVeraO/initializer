import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/** The authenticated console shell: account menu and side nav. */
export class ShellPage {
  readonly accountEmailLocator: Locator;
  readonly operatorBadge: Locator;
  readonly signOutButton: Locator;
  readonly roleBadge: Locator;
  readonly mainMenu: Locator;
  readonly moduleSections: Locator;
  readonly tenantSwitcher: Locator;
  readonly accountMenu: Locator;

  constructor(private readonly page: Page) {
    this.accountEmailLocator = page.getByTestId('session-account-email');
    this.operatorBadge = page.getByTestId('session-operator');
    this.signOutButton = page.getByTestId('sign-out');
    this.roleBadge = page.getByTestId('session-role');
    this.mainMenu = page.getByTestId('menu-main');
    this.moduleSections = page.locator('[data-testid^="menu-module-"]');
    this.tenantSwitcher = page.getByTestId('topbar-tenant-switcher');
    this.accountMenu = page.getByTestId('topbar-account-menu');
  }

  /** Email of the signed-in account, once the topbar shows it. */
  async accountEmail(): Promise<string> {
    await expect(this.accountEmailLocator).toBeVisible();
    const email = (await this.accountEmailLocator.textContent())?.trim() ?? '';
    return email;
  }

  /** Clicks the side-nav entry `nav-<id>`. */
  async nav(id: string): Promise<void> {
    await this.page.getByTestId(`nav-${id}`).click();
  }

  async signOut(): Promise<void> {
    await this.signOutButton.click();
  }
}
