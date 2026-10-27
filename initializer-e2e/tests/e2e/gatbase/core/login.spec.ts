import { test, expect } from '../../../../src/fixtures/test';
import { assertGatbaseEnv, loadEnv } from '../../../../src/env';

const env = assertGatbaseEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Gatbase core: login', () => {
  // Until the member screens add /memberships, /home is the protected URL.
  test(
    'signs in to the return URL and signs out',
    { tag: ['@e2e', '@core-auth'] },
    async ({ page, loginPage, shellPage }) => {
      const { auth } = loadEnv();

      await page.goto('/home');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
      expect(new URL(page.url()).searchParams.get('returnUrl')).toBe('/home');

      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );

      await expect(shellPage.accountEmailLocator).toHaveText(auth.email);
      await expect(shellPage.operatorBadge).toBeVisible();

      await shellPage.signOut();
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');

      await page.goto('/home');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
    },
  );

  test(
    'seeds the bootstrap admin as platform operator',
    { tag: ['@e2e', '@core-auth'] },
    async ({ operator }) => {
      const { auth } = loadEnv();

      const session = await operator.session();

      expect(session.account.isPlatformOperator).toBe(true);
      expect(session.account.email).toBe(auth.email);
    },
  );
});
