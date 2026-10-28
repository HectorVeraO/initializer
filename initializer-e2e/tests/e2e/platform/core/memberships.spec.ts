import { test, expect } from '../../../../src/fixtures/test';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';
import {
  emailFor,
  passwordFor,
  uniqueId,
} from '../../../../src/helpers/platform/ids';
import { PlatformApi } from '../../../../src/helpers/platform/api';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Core: memberships', () => {
  test(
    'changes a role, suspends and reactivates a member',
    { tag: ['@e2e', '@identity'] },
    async (
      { page, playwright, loginPage, membershipsPage, newTenantAdmin },
      testInfo,
    ) => {
      const { admin, api } = await newTenantAdmin();
      const id = uniqueId(testInfo);
      const janeEmail = emailFor('jane', id);
      const token = await api.invite(janeEmail, 'member');
      await PlatformApi.acceptNew(
        playwright,
        loadEnv().web.baseUrl,
        token,
        passwordFor(id),
        `Jane ${id}`,
      );

      await page.goto('/login');
      await loginPage.signIn(admin, '/home');
      await page.goto('/memberships');
      await expect(membershipsPage.role(`jane.${id}`)).toHaveText('Miembro');

      await membershipsPage.changeRole(`jane.${id}`, 'viewer');
      await page.reload();
      await expect(membershipsPage.role(`jane.${id}`)).toHaveText('Lector');

      await membershipsPage.suspend(`jane.${id}`);
      await expect(membershipsPage.status(`jane.${id}`)).toHaveText(
        'Suspendido',
      );

      await membershipsPage.reactivate(`jane.${id}`);
      await expect(membershipsPage.status(`jane.${id}`)).toHaveText('Activo');
    },
  );
});
