import { okEnvelope } from '../../../../testing/fixtures/api-envelope';
import { sessionView } from '../../../../testing/fixtures/session-view';
import type { MembershipRole, SessionView } from '../session/session.types';
import { SessionRequirement, meetsRequirement } from './requirement';

const ROLES: readonly (MembershipRole | null)[] = [null, 'admin', 'member', 'viewer'];

const FLAG_SETS: readonly Record<string, boolean>[] = [{}, { beta: true }, { beta: false }, { other: true }];

/** Every combination of role, tenant, operator and flag set. */
function allSessions(): SessionView[] {
  const sessions: SessionView[] = [];

  for (const role of ROLES) {
    for (const hasTenant of [true, false]) {
      for (const isPlatformOperator of [true, false]) {
        for (const flags of FLAG_SETS) {
          const base = sessionView() as unknown as SessionView;

          sessions.push({ ...base, activeTenant: hasTenant ? base.activeTenant : null, flags, account: { ...base.account, role, isPlatformOperator } });
        }
      }
    }
  }

  return sessions;
}

/** Every requirement made of any subset of the clauses, each with its possible values. */
function allRequirements(): SessionRequirement[] {
  const tenants: (SessionRequirement['tenant'] | undefined)[] = [undefined, 'required', 'required-unless-operator'];

  const roles: (readonly MembershipRole[] | undefined)[] = [undefined, [], ['admin'], ['admin', 'member'], ['viewer']];

  const operators: (true | undefined)[] = [undefined, true];

  const flags: (string | undefined)[] = [undefined, 'beta'];

  const requirements: SessionRequirement[] = [];

  for (const tenant of tenants) {
    for (const role of roles) {
      for (const platformOperator of operators) {
        for (const flag of flags) {
          requirements.push({ ...(tenant && { tenant }), ...(role && { roles: role }), ...(platformOperator && { platformOperator }), ...(flag && { flag }) });
        }
      }
    }
  }

  return requirements;
}

describe('meetsRequirement', () => {
  it('passes any session when nothing is required', () => {
    for (const session of allSessions()) {
      expect(meetsRequirement(session, {})).toBe(true);
    }
  });

  it('never passes a session whose role is outside the required roles', () => {
    for (const session of allSessions()) {
      for (const requirement of allRequirements()) {
        const role = session.account.role;

        const isOutside = requirement.roles !== undefined && (role === null || !requirement.roles.includes(role));

        if (isOutside) {
          expect(meetsRequirement(session, requirement)).toBe(false);
        }
      }
    }
  });

  it('never passes a non-operator an operator requirement, nor a missing flag, nor a missing tenant', () => {
    for (const session of allSessions()) {
      for (const requirement of allRequirements()) {
        const missesOperator = requirement.platformOperator === true && !session.account.isPlatformOperator;

        const missesFlag = requirement.flag !== undefined && session.flags[requirement.flag] !== true;

        const missesTenant = session.activeTenant === null && (requirement.tenant === 'required' || (requirement.tenant === 'required-unless-operator' && !session.account.isPlatformOperator));

        if (missesOperator || missesFlag || missesTenant) {
          expect(meetsRequirement(session, requirement)).toBe(false);
        }
      }
    }
  });

  describe('hand-written outcomes', () => {
    const operatorWithoutTenant = { activeTenant: null, account: { ...(sessionView()['account'] as object), isPlatformOperator: true } };

    const cases: { name: string; overrides: Record<string, unknown>; requirement: SessionRequirement; expected: boolean }[] = [
      { name: 'tenant required, tenant present', overrides: {}, requirement: { tenant: 'required' }, expected: true },
      { name: 'tenant required, tenant absent', overrides: { activeTenant: null }, requirement: { tenant: 'required' }, expected: false },
      { name: 'tenant required, operator without tenant', overrides: operatorWithoutTenant, requirement: { tenant: 'required' }, expected: false },
      { name: 'tenant required unless operator, operator without tenant', overrides: operatorWithoutTenant, requirement: { tenant: 'required-unless-operator' }, expected: true },
      { name: 'tenant required unless operator, non-operator without tenant', overrides: { activeTenant: null }, requirement: { tenant: 'required-unless-operator' }, expected: false },
      { name: 'role listed', overrides: {}, requirement: { roles: ['admin', 'member'] }, expected: true },
      { name: 'role not listed', overrides: {}, requirement: { roles: ['viewer'] }, expected: false },
      { name: 'empty role list', overrides: {}, requirement: { roles: [] }, expected: false },
      { name: 'operator required, non-operator', overrides: {}, requirement: { platformOperator: true }, expected: false },
      { name: 'flag on', overrides: { flags: { beta: true } }, requirement: { flag: 'beta' }, expected: true },
      { name: 'flag off', overrides: { flags: { beta: false } }, requirement: { flag: 'beta' }, expected: false },
      { name: 'flag unknown', overrides: {}, requirement: { flag: 'beta' }, expected: false },
      { name: 'tenantless operator meets tenant and roles with orTenantlessOperator', overrides: operatorWithoutTenant, requirement: { tenant: 'required', roles: ['admin'], orTenantlessOperator: true }, expected: true },
      { name: 'non-operator without tenant is held despite orTenantlessOperator', overrides: { activeTenant: null }, requirement: { tenant: 'required', roles: ['admin'], orTenantlessOperator: true }, expected: false },
      {
        name: 'operator with a tenant is still held to roles with orTenantlessOperator',
        overrides: { account: { ...(sessionView()['account'] as object), isPlatformOperator: true, role: 'viewer' } },
        requirement: { tenant: 'required', roles: ['admin'], orTenantlessOperator: true },
        expected: false
      },
      { name: 'every clause met', overrides: { flags: { beta: true } }, requirement: { tenant: 'required', roles: ['admin'], flag: 'beta' }, expected: true },
      { name: 'one clause missed among several', overrides: { flags: {} }, requirement: { tenant: 'required', roles: ['admin'], flag: 'beta' }, expected: false }
    ];

    for (const testCase of cases) {
      it(`${testCase.name} -> ${testCase.expected}`, () => {
        const session = (okEnvelope(sessionView(testCase.overrides)) as { data: SessionView }).data;

        expect(meetsRequirement(session, testCase.requirement)).toBe(testCase.expected);
      });
    }
  });

  it('keeps the fixtures honest: a plain admin session with a tenant meets a tenant-and-role requirement', () => {
    const session = (okEnvelope(sessionView()) as { data: SessionView }).data;

    expect(meetsRequirement(session, { tenant: 'required', roles: ['admin'] })).toBe(true);
  });
});
