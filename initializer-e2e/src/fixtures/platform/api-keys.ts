import { test as base, expect } from './tenancy';
import { ApiKeysPage } from '../../pages/platform/api-keys/ApiKeysPage';

type ApiKeysFixtures = {
  apiKeysPage: ApiKeysPage;
};

// The api-keys module's fixtures on top of the tenancy fixtures.
export const test = base.extend<ApiKeysFixtures>({
  apiKeysPage: async ({ page }, use) => {
    await use(new ApiKeysPage(page));
  },
});

export { expect };
