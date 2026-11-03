import { test, expect } from '../../../../src/fixtures/platform/identity';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

/** Release flag of the shell restyle; the global override flips the frame. */
const UI_RESTYLE = 'ui_restyle';

// The override is global: the two frame states share it, so they run in order
// and each one restores it. The second worker of the run never sees this file.
test.describe.configure({ mode: 'serial' });

test.describe('UI restyle', () => {
  test(
    'renders the typographic hero next to the sign-in form',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage }) => {
      await page.goto('/login');

      await expect(loginPage.hero).toBeVisible();
      await expect(loginPage.hero).toContainText(
        'Administra tu plataforma desde un solo lugar',
      );
      await expect(loginPage.form).toBeVisible();
      await expect(loginPage.email).toBeVisible();
      await expect(loginPage.password).toBeVisible();
      await expect(loginPage.submit).toBeVisible();
    },
  );

  test(
    'signs in to the restyled frame, opens the account menu and signs out',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage, shellPage, operator }) => {
      const { auth } = loadEnv();

      await operator.setGlobalFlag(UI_RESTYLE, true);
      try {
        await page.goto('/login');
        await loginPage.signIn(
          { email: auth.email, password: auth.password },
          '/home',
        );

        await expect(shellPage.restyledTopbar).toBeVisible();
        await expect(shellPage.legacyTopbar).toHaveCount(0);
        await expect(shellPage.accountTrigger).toBeVisible();
        // The account block lives behind the avatar, not inline in the bar.
        await expect(shellPage.accountEmailLocator).toBeHidden();

        await shellPage.openAccountMenu();
        await expect(shellPage.accountEmailLocator).toHaveText(auth.email);
        await expect(shellPage.operatorBadge).toBeVisible();

        await shellPage.signOut();
        await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
      } finally {
        await operator.clearGlobalFlag(UI_RESTYLE);
      }
    },
  );

  test(
    'keeps the legacy frame with the flag off',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage, shellPage, operator }) => {
      const { auth } = loadEnv();

      await operator.clearGlobalFlag(UI_RESTYLE);
      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );

      await expect(shellPage.legacyTopbar).toBeVisible();
      await expect(shellPage.restyledTopbar).toHaveCount(0);
      await expect(shellPage.accountTrigger).toHaveCount(0);
      await expect(shellPage.accountEmailLocator).toHaveText(auth.email);

      await shellPage.signOut();
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
    },
  );
});
