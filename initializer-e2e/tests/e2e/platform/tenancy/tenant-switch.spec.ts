import { test, expect } from '../../../../src/fixtures/platform/tenancy';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';
import { PlatformApi } from '../../../../src/helpers/platform/api';
import {
  emailFor,
  passwordFor,
  tenantSlug,
  uniqueId,
} from '../../../../src/helpers/platform/ids';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Tenancy: tenant switch', () => {
  test(
    'switches the active tenant and refetches the members',
    { tag: ['@e2e', '@tenancy'] },
    async (
      {
        page,
        playwright,
        loginPage,
        tenantSwitcher,
        membershipsPage,
        operator,
      },
      testInfo,
    ) => {
      const baseUrl = loadEnv().web.baseUrl;
      const id = uniqueId(testInfo);
      const slugA = `${tenantSlug(id)}-a`;
      const slugB = `${tenantSlug(id)}-b`;
      const adminUser = {
        email: emailFor('admin', id),
        password: passwordFor(id),
      };
      const janeEmail = emailFor('jane', id);

      const tenantA = await operator.createTenant({
        slug: slugA,
        name: `E2E ${id} A`,
        adminEmail: adminUser.email,
      });
      const tenantB = await operator.createTenant({
        slug: slugB,
        name: `E2E ${id} B`,
        adminEmail: adminUser.email,
      });
      await PlatformApi.acceptNew(
        playwright,
        baseUrl,
        tenantA.invitationToken,
        adminUser.password,
        `Admin ${id}`,
      );
      const api = await PlatformApi.signIn(playwright, baseUrl, adminUser);
      try {
        await api.accept(tenantB.invitationToken);
        await api.switchTenant(tenantB.id);
        const janeToken = await api.invite(janeEmail, 'member');
        await PlatformApi.acceptNew(
          playwright,
          baseUrl,
          janeToken,
          passwordFor(id),
          `Jane ${id}`,
        );
        await api.switchTenant(tenantA.id);
      } finally {
        await api.dispose();
      }

      await page.goto('/login');
      await loginPage.signIn(adminUser, '/home');
      await expect.poll(() => tenantSwitcher.activeTenantSlug()).toBe(slugA);

      await page.goto('/memberships');
      await expect(membershipsPage.row(`admin.${id}`)).toBeVisible();
      await expect(membershipsPage.row(`jane.${id}`)).toHaveCount(0);

      await tenantSwitcher.switchTo(slugB);
      await expect.poll(() => tenantSwitcher.activeTenantSlug()).toBe(slugB);
      await expect(membershipsPage.row(`jane.${id}`)).toBeVisible();

      await tenantSwitcher.switchTo(slugA);
      await expect.poll(() => tenantSwitcher.activeTenantSlug()).toBe(slugA);
      await expect(membershipsPage.row(`jane.${id}`)).toHaveCount(0);
    },
  );
});
