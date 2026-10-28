import { test as base, expect } from '@playwright/test';
import { ExamplePage } from '../pages/ExamplePage';
import { OneTimeSecretDialog } from '../pages/platform/core/OneTimeSecretDialog';
import { TenantSwitcher } from '../pages/platform/tenancy/TenantSwitcher';

type Pages = {
  examplePage: ExamplePage;
  secretDialog: OneTimeSecretDialog;
  tenantSwitcher: TenantSwitcher;
};

// Custom test that injects Page Objects. Specs import { test, expect } from here or from a
// fixtures/platform/<module> file that extends it, NEVER from '@playwright/test' directly.
// Add real POMs as fixtures as you build them.
export const test = base.extend<Pages>({
  examplePage: async ({ page }, use) => {
    await use(new ExamplePage(page));
  },
  secretDialog: async ({ page }, use) => {
    await use(new OneTimeSecretDialog(page));
  },
  tenantSwitcher: async ({ page }, use) => {
    await use(new TenantSwitcher(page));
  },
});

export { expect };
