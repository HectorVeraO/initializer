import { test as base, expect } from '@playwright/test';
import { ExamplePage } from '../pages/ExamplePage';

type Pages = {
  examplePage: ExamplePage;
};

// Custom test that injects Page Objects. Specs import { test, expect } from here,
// NEVER from '@playwright/test' directly. Add real POMs as fixtures as you build them.
export const test = base.extend<Pages>({
  examplePage: async ({ page }, use) => {
    await use(new ExamplePage(page));
  },
});

export { expect };
