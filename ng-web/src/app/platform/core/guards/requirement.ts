import type { MembershipRole, SessionView } from '../session/session.types';

/** What a route or a nav entry requires from the session. Empty object = authenticated only. */
export interface SessionRequirement {
  readonly tenant?: 'required' | 'required-unless-operator';
  readonly roles?: readonly MembershipRole[];
  readonly platformOperator?: true;
  readonly flag?: string;
}

export type UnmetRequirement = 'tenant' | 'platformOperator' | 'roles' | 'flag';

/** First unmet clause, in the order the guards redirect: tenant, platformOperator, roles, flag. */
export function firstUnmetRequirement(session: SessionView, requirement: SessionRequirement): UnmetRequirement | null {
  const hasTenant = session.activeTenant !== null;

  const isOperator = session.account.isPlatformOperator;

  if (requirement.tenant === 'required' && !hasTenant) {
    return 'tenant';
  }

  if (requirement.tenant === 'required-unless-operator' && !hasTenant && !isOperator) {
    return 'tenant';
  }

  if (requirement.platformOperator === true && !isOperator) {
    return 'platformOperator';
  }

  const role = session.account.role;

  if (requirement.roles !== undefined && (role === null || !requirement.roles.includes(role))) {
    return 'roles';
  }

  if (requirement.flag !== undefined && session.flags[requirement.flag] !== true) {
    return 'flag';
  }

  return null;
}

/** Pure check shared by guards and the menu. */
export function meetsRequirement(session: SessionView, requirement: SessionRequirement): boolean {
  const unmet = firstUnmetRequirement(session, requirement);

  return unmet === null;
}
