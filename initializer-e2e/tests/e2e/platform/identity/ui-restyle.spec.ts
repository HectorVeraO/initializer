import { test, expect } from '../../../../src/fixtures/platform/parameters';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';
import {
  emailFor,
  tenantSlug,
  uniqueId,
} from '../../../../src/helpers/platform/ids';
import type { Page } from '@playwright/test';
import type { SeedUser } from '../../../../src/helpers/platform/api';
import type { LoginPage } from '../../../../src/pages/platform/identity/LoginPage';
import type { ShellPage } from '../../../../src/pages/platform/identity/ShellPage';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

/**
 * Signs in again and waits for the shell frame. Discarding the cookies makes
 * the next sign-in create a new session, so the frame it renders is named
 * before any screen is asserted.
 */
async function signInToShell(
  page: Page,
  shellPage: ShellPage,
  loginPage: LoginPage,
  user: SeedUser,
): Promise<void> {
  await page.context().clearCookies();
  await page.goto('/login');
  await loginPage.signIn(user, '/home');
  await expect(shellPage.topbar).toBeVisible();
}

// The console screens share the seeded state, so they run in order.
test.describe.configure({ mode: 'serial' });

test.describe('Console shell', () => {
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
    'signs in to the console frame, opens the account menu and signs out',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage, shellPage }) => {
      const { auth } = loadEnv();

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );

      await expect(shellPage.topbar).toBeVisible();
      await expect(shellPage.accountTrigger).toBeVisible();
      // The account block lives behind the avatar, not inline in the bar.
      await expect(shellPage.accountEmailLocator).toBeHidden();

      await shellPage.openAccountMenu();
      await expect(shellPage.accountEmailLocator).toHaveText(auth.email);
      await expect(shellPage.operatorBadge).toBeVisible();

      await shellPage.signOut();
      await expect.poll(() => new URL(page.url()).pathname).toBe('/login');
    },
  );
});

test.describe('Module pages', () => {
  test.describe.configure({ mode: 'serial' });

  test(
    'operator screens: tenants and flags show the shared recipes and open their dialogs',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage, shellPage, operatorTenantsPage }, testInfo) => {
      const { auth } = loadEnv();
      const id = uniqueId(testInfo);
      const slug = tenantSlug(id);

      await signInToShell(page, shellPage, loginPage, auth);

      await page.goto('/operator/tenants');
      await expect(page.getByTestId('tenants-list-card')).toBeVisible();
      await expect(page.getByTestId('tenants-suspension-note')).toBeVisible();
      await operatorTenantsPage.create(
        slug,
        `E2E ${id}`,
        emailFor('owner', id),
      );
      await expect(page.getByTestId('one-time-secret-value')).toBeVisible();
      await page.getByTestId('one-time-secret-close').click();
      await expect(operatorTenantsPage.status(slug)).toHaveText('Activo');

      await page.goto('/operator/feature-flags');
      await expect(page.getByTestId('flags-list-card')).toBeVisible();
      await expect(page.getByTestId('flags-row').first()).toBeVisible();
      await page.getByTestId('flag-open').first().click();
      await expect(page.getByTestId('flag-detail-summary-card')).toBeVisible();
      await expect(page.getByTestId('flag-tenant-list-card')).toBeVisible();
      await page.getByTestId('flag-tenant-add').click();
      await expect(page.getByTestId('flag-tenant-select')).toBeVisible();
      await expect(page.getByTestId('flag-tenant-enabled')).toBeVisible();
      await expect(page.getByTestId('flag-tenant-save')).toBeVisible();
      await page.getByTestId('flag-tenant-cancel').click();
      await expect(page.getByTestId('flag-tenant-select')).toHaveCount(0);
    },
  );

  test(
    'tenant screens: keys, invitations, members and parameters show the shared recipes and open their dialogs',
    { tag: ['@e2e', '@ui-restyle'] },
    async (
      {
        page,
        loginPage,
        shellPage,
        tenantSwitcher,
        operator,
        invitationsPage,
        membershipsPage,
        parametersPage,
        newTenantAdmin,
      },
      testInfo,
    ) => {
      const id = uniqueId(testInfo);
      // The definitions table pages the fetched rows, so the row under test sorts first.
      const key = `aaa.e2e.p${id}.max_attempts`;
      const { tenant, admin } = await newTenantAdmin();

      await operator.createParameter({
        key,
        valueType: 'number',
        defaultValue: 3,
        minValue: 1,
        maxValue: 10,
        description: 'e2e',
      });

      try {
        await signInToShell(page, shellPage, loginPage, admin);

        // Every screen below is tenant-scoped: the guard sends a session with
        // no active tenant to /no-tenant before the screen renders.
        await tenantSwitcher.switchTo(tenant.slug);
        await expect
          .poll(() => tenantSwitcher.activeTenantSlug())
          .toBe(tenant.slug);

        await page.goto('/api-keys');
        await expect(page.getByTestId('api-keys-list-card')).toBeVisible();
        await expect(page.getByTestId('api-keys-owner')).toBeVisible();
        await page.getByTestId('api-key-new').click();
        await expect(page.getByTestId('api-key-name')).toBeVisible();
        await expect(page.getByTestId('api-key-scope').first()).toBeVisible();
        await expect(page.getByTestId('api-key-submit')).toBeVisible();
        await page.getByTestId('api-key-cancel').click();
        await expect(page.getByTestId('api-key-name')).toHaveCount(0);

        await page.goto('/invitations');
        await expect(page.getByTestId('invitations-list-card')).toBeVisible();
        await page.getByTestId('invite-new').click();
        await expect(invitationsPage.email).toBeVisible();
        await expect(page.getByTestId('invite-role')).toBeVisible();
        await page.getByTestId('invite-cancel').click();
        await expect(invitationsPage.email).toHaveCount(0);

        await page.goto('/memberships');
        await expect(page.getByTestId('memberships-list-card')).toBeVisible();
        await expect(
          membershipsPage.row(admin.email.split('@')[0]),
        ).toBeVisible();

        await page.goto('/parameters');
        await expect(page.getByTestId('parameters-list-card')).toBeVisible();
        await expect(parametersPage.value(key)).toHaveText('3');
        await parametersPage.openOverride(key);
        await page.getByTestId('parameter-override-cancel').click();
        await expect(page.getByTestId('parameter-override-value')).toHaveCount(
          0,
        );
      } finally {
        await operator.deleteParameter(key);
      }
    },
  );
});
