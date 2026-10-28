import { TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { byTestId, configureFeatureTest, settle, textOf } from '@/core/testing/ui-testing';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../../testing/fixtures/access-testing';
import { PLATFORM_MODULES } from '@/platform/core';
import type { AccessContext, NavEntry, WebModule } from '@/platform/core';
import { NoTenantPage } from './no-tenant-page.component';

const OPERATOR = accessContext({ activeTenant: null, role: null, isPlatformOperator: true, memberships: [], account: { id: 'a', email: 'op@example.com', fullName: null } });

const MEMBER = accessContext({ activeTenant: null, role: null, isPlatformOperator: false, memberships: [], account: { id: 'b', email: 'jane@example.com', fullName: null } });

/** A synthetic module whose nav labels come from its own scope, like a real module. */
function moduleWith(id: string, title: string, nav: readonly NavEntry[]): WebModule {
  const labels = Object.fromEntries(nav.map((entry) => [entry.id, `${title} ${entry.id}`]));

  return {
    id,
    shellRoutes: [],
    publicRoutes: [],
    nav: nav.map((entry) => ({ ...entry, labelKey: `nav.${entry.id}` })),
    i18n: { scope: `${id}-spec`, alias: `${id}-spec`, loader: { es: () => Promise.resolve({ nav: labels }) } }
  };
}

function entry(id: string, path: string, requires: NavEntry['requires']): NavEntry {
  return { id, labelKey: '', icon: 'x', path, requires };
}

const TENANCY = moduleWith('tenancy', 'Tenancy', [entry('memberships', '/memberships', { tenant: 'required' }), entry('operator-tenants', '/operator/tenants', { platformOperator: true })]);

const FLAGS = moduleWith('feature-flags', 'Flags', [entry('feature-flags', '/operator/feature-flags', { platformOperator: true })]);

const PARAMETERS = moduleWith('parameters', 'Params', [entry('parameters', '/parameters', { tenant: 'required-unless-operator' })]);

const KEYS = moduleWith('api-keys', 'Keys', [entry('api-keys', '/api-keys', { tenant: 'required', roles: ['admin'], orTenantlessOperator: true })]);

const CORE = moduleWith('core', 'Core', []);

describe('NoTenantPage', () => {
  let signOut: ReturnType<typeof vi.fn<() => Promise<void>>>;

  beforeEach(() => {
    signOut = vi.fn(() => Promise.resolve());
  });

  afterEach(() => {
    TestBed.resetTestingModule();
  });

  async function render(context: AccessContext, modules: readonly WebModule[] = [CORE, TENANCY]) {
    configureFeatureTest([], [...provideFakeAccess(context, { signOut: () => signOut() }), provideTenantSelection('/no-tenant'), { provide: MessageService, useValue: { add: vi.fn() } }, { provide: PLATFORM_MODULES, useValue: modules }]);

    const fixture = TestBed.createComponent(NoTenantPage);

    await settle(fixture);

    // The tenancy and identity translation scopes load asynchronously.
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Cerrar sesión');
      expect(textOf(fixture.nativeElement)).toContain('No tienes un tenant');
    });

    return fixture.nativeElement as HTMLElement;
  }

  function navIds(root: HTMLElement): string[] {
    const ids = Array.from(root.querySelectorAll('[data-testid^="no-tenant-nav-"]')).map((link) => (link.getAttribute('data-testid') ?? '').replace('no-tenant-nav-', ''));

    return ids;
  }

  it('explains the situation and shows the account email', async () => {
    const root = await render(MEMBER);

    expect(textOf(byTestId(root, 'no-tenant'))).toContain('No eres miembro activo de ningún tenant');
    expect(textOf(byTestId(root, 'no-tenant-account-email'))).toBe('jane@example.com');
  });

  it('lists the operator tenants entry, with its label and path, for a tenantless platform operator', async () => {
    const root = await render(OPERATOR);

    expect(navIds(root)).toEqual(['operator-tenants']);
    expect(byTestId(root, 'no-tenant-nav-operator-tenants')?.getAttribute('href')).toBe('/operator/tenants');
    expect(textOf(byTestId(root, 'no-tenant-nav-operator-tenants'))).toBe('Tenancy operator-tenants');
  });

  it('lists nothing for a tenantless member', async () => {
    const root = await render(MEMBER, [CORE, TENANCY, FLAGS, PARAMETERS, KEYS]);

    expect(navIds(root)).toEqual([]);
  });

  it('lists every entry a tenantless operator may open across the installed modules, in module order', async () => {
    const root = await render(OPERATOR, [CORE, TENANCY, FLAGS, PARAMETERS, KEYS]);

    expect(navIds(root)).toEqual(['operator-tenants', 'feature-flags', 'parameters', 'api-keys']);
  });

  it('lists no entry of a module that is not installed', async () => {
    const root = await render(OPERATOR, [CORE, TENANCY]);

    expect(byTestId(root, 'no-tenant-nav-feature-flags')).toBeNull();
    expect(byTestId(root, 'no-tenant-nav-api-keys')).toBeNull();
  });

  it('signs out through the access provider', async () => {
    const root = await render(MEMBER);

    (byTestId(root, 'sign-out') as HTMLButtonElement).click();

    await vi.waitFor(() => expect(signOut).toHaveBeenCalledOnce());
  });
});
