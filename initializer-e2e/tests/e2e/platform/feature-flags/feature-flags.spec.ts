import { test, expect } from '../../../../src/fixtures/platform/feature-flags';
import { assertPlatformEnv, loadEnv } from '../../../../src/env';
import {
  emailFor,
  tenantSlug,
  uniqueId,
} from '../../../../src/helpers/platform/ids';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

// The ops flag of the TCK fixture manifest the e2e stack runs with.
const OPS_FLAG = 'example_outbound_enabled';

test.describe('Feature flags', () => {
  test(
    'an operator sets a tenant override and removes it again',
    { tag: ['@e2e', '@feature-flags'] },
    async ({ page, loginPage, featureFlagsPage, operator }, testInfo) => {
      const { auth } = loadEnv();
      const id = uniqueId(testInfo);
      const slug = tenantSlug(id);
      await operator.createTenant({
        slug,
        name: `E2E ${id}`,
        adminEmail: emailFor('owner', id),
      });

      await page.goto('/login');
      await loginPage.signIn(
        { email: auth.email, password: auth.password },
        '/home',
      );
      await page.goto('/operator/feature-flags');

      await expect(featureFlagsPage.row(OPS_FLAG)).toBeVisible();
      await expect(featureFlagsPage.type(OPS_FLAG)).toHaveAttribute(
        'data-value',
        'ops',
      );
      await expect(featureFlagsPage.expires(OPS_FLAG)).toHaveText('-');

      await featureFlagsPage.open(OPS_FLAG);
      await featureFlagsPage.addTenantOverride(slug, false);
      await expect(featureFlagsPage.tenantRow(slug)).toBeVisible();
      await expect(featureFlagsPage.tenantEnabled(slug)).toHaveAttribute(
        'data-value',
        'false',
      );

      await featureFlagsPage.removeTenantOverride(slug);
      await expect(featureFlagsPage.tenantRow(slug)).toHaveCount(0);
    },
  );
});
