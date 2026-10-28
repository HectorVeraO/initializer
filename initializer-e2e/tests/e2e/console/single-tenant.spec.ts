import { test, expect } from '../../../src/fixtures/platform/identity';
import { assertPlatformEnv, loadEnv } from '../../../src/env';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Console with identity and no tenancy', () => {
  test(
    'the bootstrap operator signs in to a single-tenant console',
    { tag: ['@e2e', '@console-identity'] },
    async ({ page, loginPage, shellPage }) => {
      const { auth } = loadEnv();

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );

      await expect(shellPage.tenantSwitcher).toHaveCount(0);
      await expect(shellPage.accountEmailLocator).toHaveText(auth.email);
      await expect(shellPage.operatorBadge).toBeVisible();
      await expect(shellPage.roleBadge).toHaveCount(0);
    },
  );

  test(
    'the tenancy pages are not found',
    { tag: ['@e2e', '@console-identity'] },
    async ({ page, loginPage }) => {
      const { auth } = loadEnv();

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );

      await page.goto('/memberships');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/notfound');
    },
  );
});
