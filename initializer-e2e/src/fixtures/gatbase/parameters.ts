import { test as base, expect } from '../test';
import { ParametersPage } from '../../pages/gatbase/parameters/ParametersPage';

type ParametersFixtures = {
  parametersPage: ParametersPage;
};

// The parameters module's fixtures on top of the shared test: copied with the module.
export const test = base.extend<ParametersFixtures>({
  parametersPage: async ({ page }, use) => {
    await use(new ParametersPage(page));
  },
});

export { expect };
