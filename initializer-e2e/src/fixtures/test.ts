import { test as base, expect } from '@playwright/test';
import { loadEnv } from '../env';
import { GatbaseApi } from '../helpers/gatbase/api';
import { ExamplePage } from '../pages/ExamplePage';
import { LoginPage } from '../pages/gatbase/core/LoginPage';
import { ShellPage } from '../pages/gatbase/core/ShellPage';

type Pages = {
  examplePage: ExamplePage;
  loginPage: LoginPage;
  shellPage: ShellPage;
};

type Seeds = {
  /** The bootstrap admin (platform operator) with its own API session. */
  operator: GatbaseApi;
};

// Custom test that injects Page Objects. Specs import { test, expect } from here,
// NEVER from '@playwright/test' directly. Add real POMs as fixtures as you build them.
export const test = base.extend<Pages & Seeds>({
  examplePage: async ({ page }, use) => {
    await use(new ExamplePage(page));
  },
  loginPage: async ({ page }, use) => {
    await use(new LoginPage(page));
  },
  shellPage: async ({ page }, use) => {
    await use(new ShellPage(page));
  },
  operator: async ({ playwright }, use) => {
    const env = loadEnv();
    const api = await GatbaseApi.signIn(playwright, env.web.baseUrl, {
      email: env.auth.email,
      password: env.auth.password,
    });
    await use(api);
    await api.dispose();
  },
});

export { expect };
