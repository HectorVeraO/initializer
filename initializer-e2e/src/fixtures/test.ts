import { test as base, expect } from '@playwright/test';
import { loadEnv } from '../env';
import { PlatformApi } from '../helpers/platform/api';
import type { SeedTenant, SeedUser } from '../helpers/platform/api';
import {
  emailFor,
  passwordFor,
  tenantSlug,
  uniqueId,
} from '../helpers/platform/ids';
import { ExamplePage } from '../pages/ExamplePage';
import { AcceptInvitationPage } from '../pages/platform/core/AcceptInvitationPage';
import { ApiKeysPage } from '../pages/platform/core/ApiKeysPage';
import { InvitationsPage } from '../pages/platform/core/InvitationsPage';
import { OneTimeSecretDialog } from '../pages/platform/core/OneTimeSecretDialog';
import { MembershipsPage } from '../pages/platform/core/MembershipsPage';
import { OperatorTenantsPage } from '../pages/platform/core/OperatorTenantsPage';
import { LoginPage } from '../pages/platform/core/LoginPage';
import { ShellPage } from '../pages/platform/core/ShellPage';

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
  operator: PlatformApi;
  /** Creates tenant e2e-<id> whose first admin is a fresh user created through accept-new, and signs that user in over the API. */
  newTenantAdmin: () => Promise<{
    readonly tenant: SeedTenant;
    readonly admin: SeedUser;
    readonly api: PlatformApi;
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
    const api = await PlatformApi.signIn(playwright, env.web.baseUrl, {
      email: env.auth.email,
      password: env.auth.password,
    });
    await use(api);
    await api.dispose();
  },
  newTenantAdmin: async ({ playwright, operator }, use, testInfo) => {
    const env = loadEnv();
    const sessions: PlatformApi[] = [];
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
      await PlatformApi.acceptNew(
        playwright,
        env.web.baseUrl,
        created.invitationToken,
        adminUser.password,
        `Admin ${id}`,
      );
      const api = await PlatformApi.signIn(
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
