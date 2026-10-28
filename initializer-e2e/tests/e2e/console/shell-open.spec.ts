import { test, expect } from '../../../src/fixtures/test';
import { checkEnvKeys } from '../../../src/env';
import { ShellPage } from '../../../src/pages/platform/identity/ShellPage';

// Needs no credentials: a skip here would pass by accident, so only the base URL is guarded.
const env = checkEnvKeys(['INIT_WEB_BASE_URL']);

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Console without identity', () => {
  test(
    'opens on the home page without signing in',
    { tag: ['@e2e', '@shell-open'] },
    async ({ page }) => {
      const shell = new ShellPage(page);

      await page.goto('/');

      await expect.poll(() => new URL(page.url()).pathname).toBe('/home');
      await expect(shell.mainMenu).toContainText('Inicio');
      await expect(shell.moduleSections).toHaveCount(0);
      // The topbar renders (theme toggle), so the absences below are not vacuous.
      await expect(page.getByTestId('theme-toggle')).toBeVisible();
      await expect(shell.tenantSwitcher).toHaveCount(0);
      await expect(shell.accountMenu).toHaveCount(0);
    },
  );
});
