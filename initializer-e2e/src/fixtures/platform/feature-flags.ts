import { test as base, expect } from './tenancy';
import { FeatureFlagsPage } from '../../pages/platform/feature-flags/FeatureFlagsPage';

type FeatureFlagsFixtures = {
  featureFlagsPage: FeatureFlagsPage;
};

// The feature-flags module's fixtures on top of the tenancy fixtures: copied with the module.
export const test = base.extend<FeatureFlagsFixtures>({
  featureFlagsPage: async ({ page }, use) => {
    await use(new FeatureFlagsPage(page));
  },
});

export { expect };
