import { test as base, expect } from '@playwright/test';
import { loadEnv } from '../env';
import { GatbaseApi } from '../helpers/gatbase/api';
import type { SeedTenant, SeedUser } from '../helpers/gatbase/api';
import {
  emailFor,
  passwordFor,
  tenantSlug,
  uniqueId,
} from '../helpers/gatbase/ids';
import { ExamplePage } from '../pages/ExamplePage';
import { AcceptInvitationPage } from '../pages/gatbase/core/AcceptInvitationPage';
import { ApiKeysPage } from '../pages/gatbase/core/ApiKeysPage';
import { InvitationsPage } from '../pages/gatbase/core/InvitationsPage';
import { OneTimeSecretDialog } from '../pages/gatbase/core/OneTimeSecretDialog';
import { MembershipsPage } from '../pages/gatbase/core/MembershipsPage';
import { OperatorTenantsPage } from '../pages/gatbase/core/OperatorTenantsPage';
import { LoginPage } from '../pages/gatbase/core/LoginPage';
import { ShellPage } from '../pages/gatbase/core/ShellPage';

type Pages = {
  examplePage: ExamplePage;
  loginPage: LoginPage;
  shellPage: ShellPage;
  acceptInvitationPage: AcceptInvitationPage;
  invitationsPage: InvitationsPage;
  apiKeysPage: ApiKeysPage;
  membershipsPage: MembershipsPage;
  operatorTenantsPage: OperatorTenantsPage;
  secretDialog: OneTimeSecretDialog;
};

type Seeds = {
  /** The bootstrap admin (platform operator) with its own API session. */
  operator: GatbaseApi;
  /** Creates tenant e2e-<id> whose first admin is a fresh user created through accept-new, and signs that user in over the API. */
  newTenantAdmin: () => Promise<{
    readonly tenant: SeedTenant;
    readonly admin: SeedUser;
    readonly api: GatbaseApi;
  }>;
};

// Custom test that injects Page Objects. Specs import { test, expect } from here,
// NEVER from '@playwright/test' directly. Add real POMs as fixtures as you build them.
export const test = base.extend<Pages & Seeds>({
  examplePage: async ({ page }, use) => {
    await use(new ExamplePage(page));
  },
  acceptInvitationPage: async ({ page }, use) => {
    await use(new AcceptInvitationPage(page));
  },
  apiKeysPage: async ({ page }, use) => {
    await use(new ApiKeysPage(page));
  },
  invitationsPage: async ({ page }, use) => {
    await use(new InvitationsPage(page));
  },
  membershipsPage: async ({ page }, use) => {
    await use(new MembershipsPage(page));
  },
  operatorTenantsPage: async ({ page }, use) => {
    await use(new OperatorTenantsPage(page));
  },
  secretDialog: async ({ page }, use) => {
    await use(new OneTimeSecretDialog(page));
  },
  loginPage: async ({ page }, use) => {
    await use(new LoginPage(page));
  },
  shellPage: async ({ page }, use) => {
    await use(new ShellPage(page));
  },
  operator: async ({ playwright }, use) => {
    const env = loadEnv();
    const api = await GatbaseApi.signIn(playwright, env.web.baseUrl, {
      email: env.auth.email,
      password: env.auth.password,
    });
    await use(api);
    await api.dispose();
  },
  newTenantAdmin: async ({ playwright, operator }, use, testInfo) => {
    const env = loadEnv();
    const sessions: GatbaseApi[] = [];
    await use(async () => {
      const id = uniqueId(testInfo);
      const adminUser = {
        email: emailFor('admin', id),
        password: passwordFor(id),
      };
      const created = await operator.createTenant({
        slug: tenantSlug(id),
        name: `E2E ${id}`,
        adminEmail: adminUser.email,
      });
      await GatbaseApi.acceptNew(
        playwright,
        env.web.baseUrl,
        created.invitationToken,
        adminUser.password,
        `Admin ${id}`,
      );
      const api = await GatbaseApi.signIn(
        playwright,
        env.web.baseUrl,
        adminUser,
      );
      sessions.push(api);
      return { tenant: created, admin: adminUser, api };
    });
    for (const session of sessions) {
      await session.dispose();
    }
  },
});

export { expect };
