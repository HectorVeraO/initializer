import { test, expect } from '../../../src/fixtures/platform/api-keys';
import { assertPlatformEnv, loadEnv } from '../../../src/env';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

// Spanish copy from ng-web/src/app/platform/api-keys/i18n/es.json (defaultLang is es).
const OWNER_GLOBAL = 'Claves globales (sin tenant)';
const NO_SCOPES_WITHOUT_TENANT =
  'En este proyecto, ningún permiso puede ser tenido por una clave sin tenant.';

test.describe('Console with api-keys and no tenancy', () => {
  test(
    'the tenantless operator is told no scope can be held by a key without a tenant',
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

      // No module of this project registers a scope of level any, so nothing is offered.
      await expect(apiKeysPage.scopesEmpty).toHaveText(
        NO_SCOPES_WITHOUT_TENANT,
      );
      await expect(apiKeysPage.scopeBoxes).toHaveCount(0);
      await expect(apiKeysPage.submit).toBeDisabled();
    },
  );
});
