import { test, expect } from '../../../../src/fixtures/test';
import { assertPlatformEnv } from '../../../../src/env';
import { uniqueId } from '../../../../src/helpers/platform/ids';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Core: API keys', () => {
  test(
    'creates a key, shows the secret once and revokes it',
    { tag: ['@e2e', '@identity'] },
    async (
      { page, loginPage, shellPage, apiKeysPage, secretDialog, newTenantAdmin },
      testInfo,
    ) => {
      const { admin } = await newTenantAdmin();
      const name = `e2e-${uniqueId(testInfo)}`;

      await page.goto('/login');
      await loginPage.signIn(admin, '/home');
      await shellPage.nav('api-keys');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/api-keys');

      await apiKeysPage.create(name, 'tenant:read');
      // The value stays in this local only: never logged, attached or written.
      const plainKey = await secretDialog.secret();
      expect(plainKey).toMatch(/^ak_live_/);
      await secretDialog.close();

      await expect(apiKeysPage.row(name)).toBeVisible();
      const prefix = await apiKeysPage.prefix(name).innerText();
      expect(prefix.length).toBeGreaterThan(0);
      expect(plainKey.startsWith(prefix)).toBe(true);
      expect(await page.content()).not.toContain(plainKey);

      await apiKeysPage.revoke(name);
      await expect(apiKeysPage.status(name)).toHaveText('Revocada');
    },
  );
});
