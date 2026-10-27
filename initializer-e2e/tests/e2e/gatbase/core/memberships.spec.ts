import { test, expect } from '../../../../src/fixtures/test';
import { assertGatbaseEnv, loadEnv } from '../../../../src/env';
import {
  emailFor,
  passwordFor,
  uniqueId,
} from '../../../../src/helpers/gatbase/ids';
import { GatbaseApi } from '../../../../src/helpers/gatbase/api';

const env = assertGatbaseEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Gatbase core: memberships', () => {
  test(
    'changes a role, suspends and reactivates a member',
    { tag: ['@e2e', '@core-auth'] },
    async (
      { page, playwright, loginPage, membershipsPage, newTenantAdmin },
      testInfo,
    ) => {
      const { admin, api } = await newTenantAdmin();
      const id = uniqueId(testInfo);
      const janeEmail = emailFor('jane', id);
      const token = await api.invite(janeEmail, 'member');
      await GatbaseApi.acceptNew(
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
