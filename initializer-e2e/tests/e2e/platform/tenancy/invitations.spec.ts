import { test, expect } from '../../../../src/fixtures/platform/tenancy';
import { assertPlatformEnv } from '../../../../src/env';
import {
  emailFor,
  passwordFor,
  tenantSlug,
  uniqueId,
} from '../../../../src/helpers/platform/ids';
import { AcceptInvitationPage } from '../../../../src/pages/platform/tenancy/AcceptInvitationPage';
import { LoginPage } from '../../../../src/pages/platform/identity/LoginPage';
import { ShellPage } from '../../../../src/pages/platform/identity/ShellPage';
import { TenantSwitcher } from '../../../../src/pages/platform/tenancy/TenantSwitcher';
import {
  closeAndAttachVideo,
  newRecordedContext,
} from '../../../../src/utils/recorded-context';

const env = assertPlatformEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Tenancy: invitations', () => {
  test(
    'invites a member, shows the link once and lets the invitee create an account',
    { tag: ['@e2e', '@tenancy'] },
    async (
      {
        page,
        browser,
        loginPage,
        shellPage,
        invitationsPage,
        membershipsPage,
        secretDialog,
        newTenantAdmin,
      },
      testInfo,
    ) => {
      const { tenant, admin } = await newTenantAdmin();
      const id = uniqueId(testInfo);
      const janeEmail = emailFor('jane', id);
      const janePassword = passwordFor(id);

      await page.goto('/login');
      await loginPage.signIn(admin, '/home');
      await shellPage.nav('invitations');
      await expect
        .poll(() => new URL(page.url()).pathname)
        .toBe('/invitations');

      await invitationsPage.invite(janeEmail, 'member');
      const invitationUrl = await secretDialog.secret();
      expect(invitationUrl).toMatch(
        /\/invitations\/accept#token=[A-Za-z0-9_-]{43}$/,
      );
      await secretDialog.close();

      await expect(invitationsPage.status(`jane.${id}`)).toHaveText(
        'Pendiente',
      );
      expect(await page.content()).not.toContain(invitationUrl);

      const context = await newRecordedContext(browser);
      const guest = await context.newPage();
      try {
        await guest.goto(invitationUrl);
        const accept = new AcceptInvitationPage(guest);
        await expect(accept.fullName).toBeVisible();
        expect(accept.location()).toEqual({
          pathname: '/invitations/accept',
          hash: '',
        });

        await accept.createNewAccount(`Jane ${id}`, janePassword);
        await expect.poll(() => new URL(guest.url()).pathname).toBe('/login');
        expect(guest.url()).not.toContain(janeEmail);

        const guestLogin = new LoginPage(guest);
        await expect(guestLogin.email).toHaveValue(janeEmail);
        await guestLogin.signIn(
          { email: janeEmail, password: janePassword },
          '/home',
        );

        const guestShell = new ShellPage(guest);
        const guestSwitcher = new TenantSwitcher(guest);
        await expect
          .poll(() => guestSwitcher.activeTenantSlug())
          .toBe(tenant.slug);
        expect(await guestShell.accountEmail()).toBe(janeEmail);
      } finally {
        await closeAndAttachVideo(context, 'invitee');
      }

      await page.goto('/memberships');
      await page.reload();
      await expect(membershipsPage.role(`jane.${id}`)).toHaveText('Miembro');
      await expect(membershipsPage.status(`jane.${id}`)).toHaveText('Activo');
    },
  );

  test(
    'keeps the invitation token out of the page URL once the link opens',
    { tag: ['@e2e', '@tenancy'] },
    async ({ page, acceptInvitationPage, operator }, testInfo) => {
      const id = uniqueId(testInfo);
      const created = await operator.createTenant({
        slug: tenantSlug(id),
        name: `E2E ${id}`,
        adminEmail: emailFor('admin', id),
      });

      await page.goto(`/invitations/accept#token=${created.invitationToken}`);
      await expect(acceptInvitationPage.fullName).toBeVisible();

      expect(page.url()).not.toContain(created.invitationToken);
      expect(acceptInvitationPage.location()).toEqual({
        pathname: '/invitations/accept',
        hash: '',
      });
    },
  );
});
