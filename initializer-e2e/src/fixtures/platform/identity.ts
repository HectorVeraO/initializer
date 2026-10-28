import { test as base, expect } from '../test';
import { loadEnv } from '../../env';
import { PlatformApi } from '../../helpers/platform/api';
import { LoginPage } from '../../pages/platform/identity/LoginPage';
import { ShellPage } from '../../pages/platform/identity/ShellPage';

type IdentityFixtures = {
  loginPage: LoginPage;
  shellPage: ShellPage;
  /** The bootstrap admin (platform operator) with its own API session. */
  operator: PlatformApi;
};

// The identity module's fixtures on top of the shared test.
export const test = base.extend<IdentityFixtures>({
  loginPage: async ({ page }, use) => {
    await use(new LoginPage(page));
  },
  shellPage: async ({ page }, use) => {
    await use(new ShellPage(page));
  },
  operator: async ({ playwright }, use) => {
    const env = loadEnv();
    const api = await PlatformApi.signIn(playwright, env.web.baseUrl, {
      email: env.auth.email,
      password: env.auth.password,
    });
    await use(api);
    await api.dispose();
  },
});

export { expect };
