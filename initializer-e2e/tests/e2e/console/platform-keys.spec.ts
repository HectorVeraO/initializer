import { test, expect } from '../../../src/fixtures/platform/api-keys';
import { assertPlatformEnv, loadEnv } from '../../../src/env';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

// Spanish copy from ng-web/src/app/platform/api-keys/i18n/es.json (defaultLang is es).
const OWNER_GLOBAL = 'Claves globales (sin tenant)';
const SCOPE_LEVEL_MISMATCH =
  'Un permiso de nivel tenant no se puede otorgar a una clave sin tenant.';

test.describe('Console with api-keys and no tenancy', () => {
  test(
    'the tenantless operator cannot mint a key with a tenant-level scope',
    { tag: ['@e2e', '@console-platform-keys'] },
    async ({ page, loginPage, shellPage, apiKeysPage }) => {
      const { auth } = loadEnv();

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );
      await shellPage.nav('api-keys');
      await expect(apiKeysPage.owner).toHaveText(OWNER_GLOBAL);

      await apiKeysPage.create('console-global-key', 'api-keys:read');

      await expect(apiKeysPage.scopesError).toHaveText(SCOPE_LEVEL_MISMATCH);
      await expect(apiKeysPage.secretDialog).toHaveCount(0);
      await expect(apiKeysPage.row('console-global-key')).toHaveCount(0);
    },
  );
});
