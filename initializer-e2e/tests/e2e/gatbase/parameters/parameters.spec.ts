import { test, expect } from '../../../../src/fixtures/gatbase/parameters';
import { assertGatbaseEnv } from '../../../../src/env';
import { uniqueId } from '../../../../src/helpers/gatbase/ids';

const env = assertGatbaseEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Gatbase parameters', () => {
  let key = '';

  test.afterEach(async ({ operator }) => {
    if (key !== '') {
      await operator.deleteParameter(key);
      key = '';
    }
  });

  test(
    'an admin overrides a parameter for the tenant and clears it again',
    { tag: ['@e2e', '@parameters'] },
    async (
      { page, loginPage, shellPage, parametersPage, operator, newTenantAdmin },
      testInfo,
    ) => {
      key = `e2e.p${uniqueId(testInfo)}.max_attempts`;
      await operator.createParameter({
        key,
        valueType: 'number',
        defaultValue: 3,
        minValue: 1,
        maxValue: 10,
        description: 'e2e',
      });
      const { admin } = await newTenantAdmin();

      await page.goto('/login');
      await loginPage.signIn(admin, '/home');
      await shellPage.nav('parameters');
      await expect.poll(() => new URL(page.url()).pathname).toBe('/parameters');

      await expect(parametersPage.value(key)).toHaveText('3');
      await expect(parametersPage.source(key)).toHaveText('Global');

      await parametersPage.setOverride(key, 5);
      await expect(parametersPage.value(key)).toHaveText('5');
      await expect(parametersPage.source(key)).toHaveText('Tenant');

      await parametersPage.clearOverride(key);
      await expect(parametersPage.value(key)).toHaveText('3');
      await expect(parametersPage.source(key)).toHaveText('Global');
    },
  );
});
