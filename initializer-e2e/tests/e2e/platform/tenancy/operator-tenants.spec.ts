import { test, expect } from '../../../../src/fixtures/platform/tenancy';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';
import {
  emailFor,
  tenantSlug,
  uniqueId,
} from '../../../../src/helpers/platform/ids';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Tenancy: operator tenants', () => {
  test(
    'creates a tenant with its first-admin link, suspends and activates it',
    { tag: ['@e2e', '@tenancy'] },
    async (
      { page, loginPage, operatorTenantsPage, secretDialog },
      testInfo,
    ) => {
      const { auth } = loadEnv();
      const id = uniqueId(testInfo);
      const slug = tenantSlug(id);

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );
      await page.goto('/operator/tenants');
      await expect(page.getByTestId('nav-operator-tenants')).toBeVisible();

      await operatorTenantsPage.create(
        slug,
        `E2E ${id}`,
        emailFor('owner', id),
      );
      expect(await secretDialog.secret()).toContain('#token=');
      await secretDialog.close();

      await expect(operatorTenantsPage.status(slug)).toHaveText('Activo');
      expect(await page.content()).not.toContain('#token=');

      await operatorTenantsPage.suspend(slug);
      await expect(operatorTenantsPage.status(slug)).toHaveText('Suspendido');

      await operatorTenantsPage.activate(slug);
      await expect(operatorTenantsPage.status(slug)).toHaveText('Activo');
    },
  );

  test(
    'redirects a tenant admin who is not a platform operator to forbidden',
    { tag: ['@e2e', '@tenancy'] },
    async ({ page, loginPage, newTenantAdmin }) => {
      const { admin } = await newTenantAdmin();

      await page.goto('/login');
      await loginPage.signIn(admin, '/home');
      await page.goto('/operator/tenants');

      await expect(page).toHaveURL(/\/forbidden$/);
    },
  );
});
