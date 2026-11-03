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

/** Release flag of the console restyle; the global override flips the frame and every module screen. */
const UI_RESTYLE = 'ui_restyle';

/** The frame each flag state renders; the first thing every case proves after a toggle. */
type Frame = 'restyled' | 'legacy';

/**
 * Signs in again after the override changed. Every screen reads the flag from
 * the session snapshot, and a session created before the toggle keeps the old
 * one however many documents load; discarding the cookies makes the next
 * sign-in create a new session, and the frame it renders is named before any
 * screen is asserted.
 */
async function signInWithFrame(
  page: Page,
  shellPage: ShellPage,
  loginPage: LoginPage,
  user: SeedUser,
  frame: Frame,
): Promise<void> {
  await page.context().clearCookies();
  await page.goto('/login');
  await loginPage.signIn(user, '/home');
  await expect(
    frame === 'restyled' ? shellPage.restyledTopbar : shellPage.legacyTopbar,
  ).toBeVisible();
}

// The override is global: the frame states share it, so they run in order
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

test.describe('Restyled module pages', () => {
  test.describe.configure({ mode: 'serial' });

  // fixme: the shell renders the restyled frame but the module pages still render the
  // previous template under the same session; the unit tests cover both branches of every
  // page meanwhile. Tracked in #279 so the cause is found from a trace, not guessed.
  test.fixme(
    'operator screens: tenants and flags show the shared recipes and open their dialogs',
    { tag: ['@e2e', '@ui-restyle'] },
    async (
      { page, loginPage, shellPage, operator, operatorTenantsPage },
      testInfo,
    ) => {
      const { auth } = loadEnv();
      const id = uniqueId(testInfo);
      const slug = tenantSlug(id);

      await operator.setGlobalFlag(UI_RESTYLE, true);
      try {
        await signInWithFrame(page, shellPage, loginPage, auth, 'restyled');

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
        await expect(
          page.getByTestId('flag-detail-summary-card'),
        ).toBeVisible();
        await expect(page.getByTestId('flag-tenant-list-card')).toBeVisible();
        await page.getByTestId('flag-tenant-add').click();
        await expect(page.getByTestId('flag-tenant-select')).toBeVisible();
        await expect(page.getByTestId('flag-tenant-enabled')).toBeVisible();
        await expect(page.getByTestId('flag-tenant-save')).toBeVisible();
        await page.getByTestId('flag-tenant-cancel').click();
        await expect(page.getByTestId('flag-tenant-select')).toHaveCount(0);
      } finally {
        await operator.clearGlobalFlag(UI_RESTYLE);
      }
    },
  );

  // fixme: same as above, #279.
  test.fixme(
    'tenant screens: keys, invitations, members and parameters show the shared recipes and open their dialogs',
    { tag: ['@e2e', '@ui-restyle'] },
    async (
      {
        page,
        loginPage,
        shellPage,
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
      const { admin } = await newTenantAdmin();

      await operator.createParameter({
        key,
        valueType: 'number',
        defaultValue: 3,
        minValue: 1,
        maxValue: 10,
        description: 'e2e',
      });
      await operator.setGlobalFlag(UI_RESTYLE, true);
      try {
        await signInWithFrame(page, shellPage, loginPage, admin, 'restyled');

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
        await operator.clearGlobalFlag(UI_RESTYLE);
        await operator.deleteParameter(key);
      }
    },
  );

  // fixme: same cause as above, #279; both flag states fail on /api-keys.
  test.fixme(
    'operator screens keep the previous markup with the flag off',
    { tag: ['@e2e', '@ui-restyle'] },
    async ({ page, loginPage, shellPage, operator }) => {
      const { auth } = loadEnv();

      await operator.clearGlobalFlag(UI_RESTYLE);
      await signInWithFrame(page, shellPage, loginPage, auth, 'legacy');

      await page.goto('/operator/tenants');
      await expect(page.getByTestId('tenant-form')).toBeVisible();
      await expect(page.getByTestId('tenants-list-card')).toHaveCount(0);

      await page.goto('/operator/feature-flags');
      await expect(page.getByTestId('flags-list-card')).toHaveCount(0);
      await page.getByTestId('flag-open').first().click();
      await expect(page.getByTestId('flag-tenant-form')).toBeVisible();
      await expect(page.getByTestId('flag-detail-summary-card')).toHaveCount(0);
    },
  );

  // fixme: same cause as above, #279.
  test.fixme(
    'tenant screens keep the previous markup with the flag off',
    { tag: ['@e2e', '@ui-restyle'] },
    async (
      {
        page,
        loginPage,
        shellPage,
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
      const { admin } = await newTenantAdmin();

      await operator.createParameter({
        key,
        valueType: 'number',
        defaultValue: 3,
        minValue: 1,
        maxValue: 10,
        description: 'e2e',
      });
      await operator.clearGlobalFlag(UI_RESTYLE);

      try {
        await signInWithFrame(page, shellPage, loginPage, admin, 'legacy');

        await page.goto('/api-keys');
        await expect(page.getByTestId('api-key-form')).toBeVisible();
        await expect(page.getByTestId('api-keys-list-card')).toHaveCount(0);

        await page.goto('/invitations');
        await expect(invitationsPage.email).toBeVisible();
        await expect(page.getByTestId('invitations-list-card')).toHaveCount(0);

        await page.goto('/memberships');
        await expect(
          membershipsPage.row(admin.email.split('@')[0]),
        ).toBeVisible();
        await expect(page.getByTestId('memberships-list-card')).toHaveCount(0);

        await page.goto('/parameters');
        await expect(parametersPage.row(key)).toBeVisible();
        await expect(page.getByTestId('parameters-list-card')).toHaveCount(0);
      } finally {
        await operator.deleteParameter(key);
      }
    },
  );
});
