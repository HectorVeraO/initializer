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
  /** The topbar of the shell. */
  readonly topbar: Locator;
  /** Circular avatar that opens the account popover of the topbar. */
  readonly accountTrigger: Locator;

  constructor(private readonly page: Page) {
    this.accountEmailLocator = page.getByTestId('session-account-email');
    this.operatorBadge = page.getByTestId('session-operator');
    this.signOutButton = page.getByTestId('sign-out');
    this.roleBadge = page.getByTestId('session-role');
    this.mainMenu = page.getByTestId('menu-main');
    this.moduleSections = page.locator('[data-testid^="menu-module-"]');
    this.tenantSwitcher = page.getByTestId('topbar-tenant-switcher');
    this.accountMenu = page.getByTestId('topbar-account-menu');
    this.topbar = page.getByTestId('shell-topbar-restyled');
    this.accountTrigger = page.getByTestId('account-menu-trigger');
  }

  /** Email of the signed-in account, once the account block is visible. */
  async accountEmail(): Promise<string> {
    await this.openAccountMenu();
    const email = (await this.accountEmailLocator.textContent())?.trim() ?? '';
    return email;
  }

  /** Reveals the account block behind the avatar of the topbar. Does nothing when it is already open. */
  async openAccountMenu(): Promise<void> {
    await expect
      .poll(
        async () =>
          (await this.accountTrigger.isVisible()) ||
          (await this.accountEmailLocator.isVisible()),
      )
      .toBe(true);
    if (
      (await this.accountTrigger.isVisible()) &&
      !(await this.accountEmailLocator.isVisible())
    ) {
      await this.accountTrigger.click();
    }
    await expect(this.accountEmailLocator).toBeVisible();
  }

  /** Clicks the side-nav entry `nav-<id>`. */
  async nav(id: string): Promise<void> {
    await this.page.getByTestId(`nav-${id}`).click();
  }

  async signOut(): Promise<void> {
    await this.openAccountMenu();
    await this.signOutButton.click();
  }
}
