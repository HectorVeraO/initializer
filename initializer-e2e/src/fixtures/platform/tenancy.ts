import { test as base, expect } from './identity';
import { loadEnv } from '../../env';
import { PlatformApi } from '../../helpers/platform/api';
import type { SeedTenant, SeedUser } from '../../helpers/platform/api';
import {
  emailFor,
  passwordFor,
  tenantSlug,
  uniqueId,
} from '../../helpers/platform/ids';
import { AcceptInvitationPage } from '../../pages/platform/tenancy/AcceptInvitationPage';
import { InvitationsPage } from '../../pages/platform/tenancy/InvitationsPage';
import { MembershipsPage } from '../../pages/platform/tenancy/MembershipsPage';
import { OperatorTenantsPage } from '../../pages/platform/tenancy/OperatorTenantsPage';

type TenancyFixtures = {
  acceptInvitationPage: AcceptInvitationPage;
  invitationsPage: InvitationsPage;
  membershipsPage: MembershipsPage;
  operatorTenantsPage: OperatorTenantsPage;
  /** Creates tenant e2e-<id> whose first admin is a fresh user created through accept-new, and signs that user in over the API. */
  newTenantAdmin: () => Promise<{
    readonly tenant: SeedTenant;
    readonly admin: SeedUser;
    readonly api: PlatformApi;
  }>;
};

// The tenancy module's fixtures on top of the identity fixtures.
export const test = base.extend<TenancyFixtures>({
  acceptInvitationPage: async ({ page }, use) => {
    await use(new AcceptInvitationPage(page));
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
