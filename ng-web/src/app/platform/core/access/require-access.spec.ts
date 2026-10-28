import { Provider, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../../testing/fixtures/access-testing';
import { NO_FLAGS } from '../flags/flag-source.port';
import type { FlagSource } from '../flags/flag-source.port';
import { ACCESS_PROVIDER, OPEN_ACCESS } from './access.port';
import type { AccessProvider } from './access.port';
import type { AccessContext, AccessRequirement } from './access.types';
import { allowsAccess, assertValidRequirement, firstUnmetClause, requireAccess } from './require-access';

const MEMBER = accessContext({ role: 'member' });

const ADMIN = accessContext();

const TENANTLESS_MEMBER = accessContext({ activeTenant: null, role: null });

const TENANTLESS_OPERATOR = accessContext({ activeTenant: null, role: null, isPlatformOperator: true });

const OPERATOR_IN_TENANT_AS_MEMBER = accessContext({ role: 'member', isPlatformOperator: true });

const KEY_ADMIN: AccessRequirement = { tenant: 'required', roles: ['admin'], orTenantlessOperator: true };

/** A flag source over a plain map, as the session's `flags` snapshot is. */
function flagsOf(flags: Record<string, boolean>): FlagSource {
  return { isEnabled: (key) => flags[key] === true };
}

const FLAGS_ON = flagsOf({ beta: true });

describe('firstUnmetClause', () => {
  it('meets an empty requirement for everyone, including no context', () => {
    expect(firstUnmetClause(null, NO_FLAGS, false, {})).toBeNull();
    expect(firstUnmetClause(null, NO_FLAGS, true, {})).toBeNull();
    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, true, {})).toBeNull();
  });

  it('fails the first declared clause for a null context, in the order tenant, platformOperator, roles, flag', () => {
    expect(firstUnmetClause(null, FLAGS_ON, true, { tenant: 'required-unless-operator', platformOperator: true, roles: ['admin'], flag: 'beta' })).toBe('tenant');
    expect(firstUnmetClause(null, FLAGS_ON, true, { platformOperator: true, roles: ['admin'], flag: 'beta' })).toBe('platformOperator');
    expect(firstUnmetClause(null, FLAGS_ON, true, { roles: ['admin'], flag: 'beta' })).toBe('roles');
    expect(firstUnmetClause(null, FLAGS_ON, true, { flag: 'beta' })).toBe('flag');
  });

  it('reports clauses in the order tenant, platformOperator, roles, flag when several are unmet', () => {
    const everything: AccessRequirement = { tenant: 'required', platformOperator: true, roles: ['admin'], flag: 'beta' };

    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, true, everything)).toBe('tenant');
    expect(firstUnmetClause(MEMBER, NO_FLAGS, true, everything)).toBe('platformOperator');
    expect(firstUnmetClause(OPERATOR_IN_TENANT_AS_MEMBER, NO_FLAGS, true, everything)).toBe('roles');
    expect(firstUnmetClause(accessContext({ isPlatformOperator: true }), NO_FLAGS, true, everything)).toBe('flag');
    expect(firstUnmetClause(accessContext({ isPlatformOperator: true }), FLAGS_ON, true, everything)).toBeNull();
  });

  it('requires an active tenant for tenant: required, even of an operator', () => {
    expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, true, { tenant: 'required' })).toBe('tenant');
    expect(firstUnmetClause(ADMIN, NO_FLAGS, true, { tenant: 'required' })).toBeNull();
  });

  it('lets an operator without a tenant through required-unless-operator', () => {
    expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, true, { tenant: 'required-unless-operator' })).toBeNull();
    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, true, { tenant: 'required-unless-operator' })).toBe('tenant');
  });

  it('meets required-unless-operator for anyone in a single-tenant console', () => {
    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, false, { tenant: 'required-unless-operator' })).toBeNull();
    expect(firstUnmetClause(MEMBER, NO_FLAGS, false, { tenant: 'required-unless-operator' })).toBeNull();
  });

  it('keeps tenant: required and roles unmet in a single-tenant console for a member', () => {
    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, false, { tenant: 'required' })).toBe('tenant');
    expect(firstUnmetClause(MEMBER, NO_FLAGS, false, { roles: ['admin'] })).toBe('roles');
    expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, false, { roles: ['admin'] })).toBe('roles');
  });

  it('passes roles only for a role in the list, and a viewer is not an admin', () => {
    expect(firstUnmetClause(ADMIN, NO_FLAGS, true, { roles: ['admin', 'member'] })).toBeNull();
    expect(firstUnmetClause(MEMBER, NO_FLAGS, true, { roles: ['admin', 'member'] })).toBeNull();
    expect(firstUnmetClause(accessContext({ role: 'viewer' }), NO_FLAGS, true, { roles: ['admin', 'member'] })).toBe('roles');
    expect(firstUnmetClause(ADMIN, NO_FLAGS, true, { roles: [] })).toBe('roles');
  });

  it('meets platformOperator only for an operator', () => {
    expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, true, { platformOperator: true })).toBeNull();
    expect(firstUnmetClause(ADMIN, NO_FLAGS, true, { platformOperator: true })).toBe('platformOperator');
  });

  it('meets a flag only when the source enables it; an unknown key is unmet', () => {
    expect(firstUnmetClause(ADMIN, FLAGS_ON, true, { flag: 'beta' })).toBeNull();
    expect(firstUnmetClause(ADMIN, FLAGS_ON, true, { flag: 'other' })).toBe('flag');
    expect(firstUnmetClause(ADMIN, NO_FLAGS, true, { flag: 'beta' })).toBe('flag');
  });

  it('leaves every flag clause unmet when the flag source knows no flags (identity without feature-flags)', () => {
    const emptySource = flagsOf({});

    expect(firstUnmetClause(ADMIN, emptySource, true, { flag: 'beta' })).toBe('flag');
    expect(firstUnmetClause(ADMIN, emptySource, false, { flag: 'anything' })).toBe('flag');
  });

  describe('orTenantlessOperator', () => {
    it('lets a tenantless operator meet tenant and roles', () => {
      expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, true, KEY_ADMIN)).toBeNull();
      expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, false, KEY_ADMIN)).toBeNull();
    });

    it('holds a member to the roles clause in a single-tenant console', () => {
      expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, false, KEY_ADMIN)).toBe('tenant');
      expect(firstUnmetClause(MEMBER, NO_FLAGS, false, KEY_ADMIN)).toBe('roles');
    });

    it('holds an operator with an active tenant to roles: role member is unmet', () => {
      expect(firstUnmetClause(OPERATOR_IN_TENANT_AS_MEMBER, NO_FLAGS, true, KEY_ADMIN)).toBe('roles');
      expect(firstUnmetClause(OPERATOR_IN_TENANT_AS_MEMBER, NO_FLAGS, false, KEY_ADMIN)).toBe('roles');
    });

    it('does not help a tenantless member', () => {
      expect(firstUnmetClause(TENANTLESS_MEMBER, NO_FLAGS, true, KEY_ADMIN)).toBe('tenant');
    });

    it('still evaluates the flag clause for a tenantless operator', () => {
      expect(firstUnmetClause(TENANTLESS_OPERATOR, NO_FLAGS, true, { ...KEY_ADMIN, flag: 'beta' })).toBe('flag');
      expect(firstUnmetClause(TENANTLESS_OPERATOR, FLAGS_ON, true, { ...KEY_ADMIN, flag: 'beta' })).toBeNull();
    });
  });
});

describe('assertValidRequirement', () => {
  it('rejects orTenantlessOperator without roles', () => {
    expect(() => assertValidRequirement({ orTenantlessOperator: true })).toThrow('PLATFORM_INVALID_REQUIREMENT');
    expect(() => assertValidRequirement({ tenant: 'required', orTenantlessOperator: true })).toThrow('PLATFORM_INVALID_REQUIREMENT');
  });

  it('rejects orTenantlessOperator together with platformOperator', () => {
    expect(() => assertValidRequirement({ roles: ['admin'], platformOperator: true, orTenantlessOperator: true })).toThrow('PLATFORM_INVALID_REQUIREMENT');
  });

  it('accepts every other requirement', () => {
    expect(() => assertValidRequirement({})).not.toThrow();
    expect(() => assertValidRequirement(KEY_ADMIN)).not.toThrow();
    expect(() => assertValidRequirement({ tenant: 'required-unless-operator', platformOperator: true, flag: 'beta' })).not.toThrow();
  });

  it('is enforced when a guard is built', () => {
    expect(() => requireAccess({ orTenantlessOperator: true })).toThrow('PLATFORM_INVALID_REQUIREMENT');
  });
});

describe('requireAccess', () => {
  afterEach(() => {
    TestBed.resetTestingModule();
  });

  /** Navigates to a route guarded by `guard` and returns where the router ended. */
  async function visit(guard: ReturnType<typeof requireAccess>, providers: Provider[]): Promise<string> {
    TestBed.resetTestingModule();
    configureFeatureTest(
      [
        { path: 'target', canMatch: [guard], component: BlankComponent },
        { path: 'login', component: BlankComponent },
        { path: 'no-tenant', component: BlankComponent },
        { path: 'forbidden', component: BlankComponent },
        { path: 'notfound', component: BlankComponent },
        { path: '**', redirectTo: 'notfound' }
      ],
      providers
    );

    const router = TestBed.inject(Router);

    await router.navigateByUrl('/target');

    return router.url;
  }

  async function outcome(requirement: AccessRequirement, context: AccessContext | null, options: { selection?: string | null; flags?: Record<string, boolean> } = {}): Promise<string> {
    const url = await visit(requireAccess(requirement), [...provideFakeAccess(context, { flags: options.flags }), provideTenantSelection(options.selection ?? null)]);

    return url;
  }

  it('sends an anonymous visitor to the sign-in redirect the provider answers', async () => {
    expect(await outcome({}, null)).toBe('/login');
  });

  it('passes an admitted visitor with an empty requirement', async () => {
    expect(await outcome({}, TENANTLESS_MEMBER)).toBe('/target');
  });

  describe('with TENANT_SELECTION bound', () => {
    it('sends a visitor without a tenant to its noTenantPath', async () => {
      expect(await outcome({ tenant: 'required' }, TENANTLESS_MEMBER, { selection: '/no-tenant' })).toBe('/no-tenant');
    });

    it('sends a tenantless member of a multi-tenant console to the noTenantPath on a required-unless-operator route', async () => {
      expect(await outcome({ tenant: 'required-unless-operator' }, TENANTLESS_MEMBER, { selection: '/no-tenant' })).toBe('/no-tenant');
    });

    it('sends a tenantless member to the noTenantPath on a KEY_ADMIN route', async () => {
      expect(await outcome(KEY_ADMIN, TENANTLESS_MEMBER, { selection: '/no-tenant' })).toBe('/no-tenant');
    });

    it('lets a tenantless operator reach a KEY_ADMIN route', async () => {
      expect(await outcome(KEY_ADMIN, TENANTLESS_OPERATOR, { selection: '/no-tenant' })).toBe('/target');
    });
  });

  describe('without TENANT_SELECTION (single-tenant console)', () => {
    it('sends a tenant clause to /forbidden', async () => {
      expect(await outcome({ tenant: 'required' }, TENANTLESS_MEMBER)).toBe('/forbidden');
    });

    it('meets required-unless-operator for a member', async () => {
      expect(await outcome({ tenant: 'required-unless-operator' }, MEMBER)).toBe('/target');
      expect(await outcome({ tenant: 'required-unless-operator' }, TENANTLESS_MEMBER)).toBe('/target');
    });

    it('sends roles to /forbidden', async () => {
      expect(await outcome({ roles: ['admin'] }, MEMBER)).toBe('/forbidden');
    });

    it('lets a tenantless operator reach a KEY_ADMIN route', async () => {
      expect(await outcome(KEY_ADMIN, TENANTLESS_OPERATOR)).toBe('/target');
    });

    it('sends a member to /forbidden on a KEY_ADMIN route', async () => {
      expect(await outcome(KEY_ADMIN, MEMBER)).toBe('/forbidden');
    });

    it('sends an operator with an active tenant and role member to /forbidden on a KEY_ADMIN route', async () => {
      expect(await outcome(KEY_ADMIN, OPERATOR_IN_TENANT_AS_MEMBER)).toBe('/forbidden');
    });
  });

  it('sends a missing platformOperator clause to /forbidden', async () => {
    expect(await outcome({ platformOperator: true }, ADMIN)).toBe('/forbidden');
    expect(await outcome({ platformOperator: true }, TENANTLESS_OPERATOR)).toBe('/target');
  });

  it('makes a path with an unmet flag look missing', async () => {
    expect(await outcome({ flag: 'beta' }, ADMIN)).toBe('/notfound');
    expect(await outcome({ flag: 'beta' }, ADMIN, { flags: { beta: true } })).toBe('/target');
    expect(await outcome({ flag: 'beta' }, ADMIN, { flags: { beta: false } })).toBe('/notfound');
  });

  it('returns the provider redirect as is, before any clause is evaluated', async () => {
    expect(await outcome({ platformOperator: true }, null)).toBe('/login');
  });

  describe('under the default OPEN_ACCESS', () => {
    it('admits an empty requirement', async () => {
      expect(await visit(requireAccess({}), [])).toBe('/target');
    });

    it('denies required-unless-operator, which no visitor can meet without a context', async () => {
      expect(await visit(requireAccess({ tenant: 'required-unless-operator' }), [])).toBe('/forbidden');
    });

    it('denies roles and platformOperator to /forbidden and a flag to a missing path', async () => {
      expect(await visit(requireAccess({ roles: ['admin'] }), [])).toBe('/forbidden');
      expect(await visit(requireAccess({ platformOperator: true }), [])).toBe('/forbidden');
      expect(await visit(requireAccess({ flag: 'beta' }), [])).toBe('/notfound');
    });

    it('reports admitted and no context', () => {
      TestBed.configureTestingModule({});
      const open = TestBed.inject(OPEN_ACCESS);

      expect(open.admitted()).toBe(true);
      expect(open.context()).toBeNull();
    });

    it('admits on admit, and reload and signOut resolve', async () => {
      TestBed.configureTestingModule({});
      const open = TestBed.inject(OPEN_ACCESS);

      await expect(open.admit()).resolves.toBe(true);
      await expect(open.reload()).resolves.toBeUndefined();
      await expect(open.signOut()).resolves.toBeUndefined();
    });

    it('rejects switchTenant with PLATFORM_NO_SESSION', async () => {
      TestBed.configureTestingModule({});

      await expect(TestBed.inject(OPEN_ACCESS).switchTenant('any-tenant')).rejects.toThrow('PLATFORM_NO_SESSION');
    });

    it('navigates signIn to /home', async () => {
      configureFeatureTest([
        { path: 'home', component: BlankComponent },
        { path: '**', component: BlankComponent }
      ]);

      await TestBed.inject(OPEN_ACCESS).signIn({});

      expect(TestBed.inject(Router).url).toBe('/home');
    });

    it('is what ACCESS_PROVIDER resolves to without a binding', () => {
      TestBed.configureTestingModule({});

      expect(TestBed.inject(ACCESS_PROVIDER)).toBe(TestBed.inject(OPEN_ACCESS));
    });
  });

  describe('security', () => {
    it('keeps enforcing the requirement it was built with after the caller mutates it', async () => {
      const roles: ('admin' | 'member' | 'viewer')[] = ['admin'];

      const requirement = { roles, tenant: 'required' } as { roles: ('admin' | 'member' | 'viewer')[]; tenant?: 'required'; platformOperator?: true; flag?: string };

      const guard = requireAccess(requirement);

      roles.push('member');
      delete requirement.tenant;
      requirement.roles = ['member'];
      requirement.platformOperator = undefined;
      requirement.flag = 'beta';

      expect(await visit(guard, [...provideFakeAccess(MEMBER), provideTenantSelection('/no-tenant')])).toBe('/forbidden');
    });

    it('never grants roles to an operator with an active tenant through orTenantlessOperator', async () => {
      const operatorAsViewer = accessContext({ role: 'viewer', isPlatformOperator: true });

      expect(await outcome(KEY_ADMIN, operatorAsViewer, { selection: '/no-tenant' })).toBe('/forbidden');
      expect(await outcome(KEY_ADMIN, operatorAsViewer)).toBe('/forbidden');
    });
  });
});

describe('allowsAccess', () => {
  const admitted = (context: AccessContext | null, isAdmitted = true): AccessProvider => ({
    admitted: signal(isAdmitted),
    context: signal(context),
    admit: () => Promise.resolve(true),
    switchTenant: () => Promise.resolve(),
    reload: () => Promise.resolve(),
    signOut: () => Promise.resolve(),
    signIn: () => Promise.resolve()
  });

  it('is false while the provider does not admit the visitor, whatever the requirement', () => {
    expect(allowsAccess(admitted(ADMIN, false), NO_FLAGS, true, {})).toBe(false);
  });

  it('is true for an admitted visitor with an empty requirement, with or without a context', () => {
    expect(allowsAccess(admitted(null), NO_FLAGS, false, {})).toBe(true);
    expect(allowsAccess(admitted(ADMIN), NO_FLAGS, true, {})).toBe(true);
  });

  it('follows the clauses', () => {
    expect(allowsAccess(admitted(MEMBER), NO_FLAGS, true, { roles: ['admin'] })).toBe(false);
    expect(allowsAccess(admitted(ADMIN), NO_FLAGS, true, { roles: ['admin'] })).toBe(true);
    expect(allowsAccess(admitted(MEMBER), NO_FLAGS, false, { tenant: 'required-unless-operator' })).toBe(true);
    expect(allowsAccess(admitted(null), NO_FLAGS, false, { tenant: 'required-unless-operator' })).toBe(false);
  });
});
