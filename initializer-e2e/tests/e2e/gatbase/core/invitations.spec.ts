import { test, expect } from '../../../../src/fixtures/test';
import { assertGatbaseEnv } from '../../../../src/env';
import {
  emailFor,
  passwordFor,
  uniqueId,
} from '../../../../src/helpers/gatbase/ids';
import { AcceptInvitationPage } from '../../../../src/pages/gatbase/core/AcceptInvitationPage';
import { LoginPage } from '../../../../src/pages/gatbase/core/LoginPage';
import { ShellPage } from '../../../../src/pages/gatbase/core/ShellPage';
import {
  closeAndAttachVideo,
  newRecordedContext,
} from '../../../../src/utils/recorded-context';

const env = assertGatbaseEnv();

test.skip(!env.ok, env.ok ? '' : `missing env: ${env.missing.join(', ')}`);

test.describe('Gatbase core: invitations', () => {
  // The designed final check on /memberships belongs to the member screens.
  test(
    'invites a member, shows the link once and lets the invitee create an account',
    { tag: ['@e2e', '@core-auth'] },
    async (
      {
        page,
        browser,
        loginPage,
        shellPage,
        invitationsPage,
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
        await expect
          .poll(() => guestShell.activeTenantSlug())
          .toBe(tenant.slug);
        expect(await guestShell.accountEmail()).toBe(janeEmail);
      } finally {
        await closeAndAttachVideo(context, 'invitee');
      }
    },
  );
});
