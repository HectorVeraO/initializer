import { test, expect } from '../../../../src/fixtures/test';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Core: login', () => {
  test(
    'signs in to the return URL and signs out',
    { tag: ['@e2e', '@identity'] },
    async ({ page, loginPage, shellPage }) => {
      const { auth } = loadEnv();

      await page.goto('/memberships');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
      expect(new URL(page.url()).searchParams.get('returnUrl')).toBe(
        '/memberships',
      );

      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/memberships',
      );

      await expect(shellPage.accountEmailLocator).toHaveText(auth.email);
      await expect(shellPage.operatorBadge).toBeVisible();
      await expect(page.getByTestId('nav-operator-tenants')).toBeVisible();

      await shellPage.signOut();
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');

      await page.goto('/home');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
    },
  );

  test(
    'seeds the bootstrap admin as platform operator',
    { tag: ['@e2e', '@identity'] },
    async ({ operator }) => {
      const { auth } = loadEnv();

      const session = await operator.session();

      expect(session.account.isPlatformOperator).toBe(true);
      expect(session.account.email).toBe(auth.email);
    },
  );
});
