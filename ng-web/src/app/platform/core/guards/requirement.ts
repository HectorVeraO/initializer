import type { AccessRequirement } from '../access/access.types';
import type { SessionView } from '../session/session.types';

/** What a route or a nav entry requires from the session: the core access requirement. */
export type SessionRequirement = AccessRequirement;

export type UnmetRequirement = 'tenant' | 'platformOperator' | 'roles' | 'flag';

/** First unmet clause, in the order the guards redirect: tenant, platformOperator, roles, flag. */
export function firstUnmetRequirement(session: SessionView, requirement: SessionRequirement): UnmetRequirement | null {
  const hasTenant = session.activeTenant !== null;

  const isOperator = session.account.isPlatformOperator;

  // A tenantless operator meets the tenant and roles clauses, as in requireAccess.
  const isTenantlessOperator = requirement.orTenantlessOperator === true && !hasTenant && isOperator;

  if (requirement.tenant === 'required' && !hasTenant && !isTenantlessOperator) {
    return 'tenant';
  }

  if (requirement.tenant === 'required-unless-operator' && !hasTenant && !isOperator) {
    return 'tenant';
  }

  if (requirement.platformOperator === true && !isOperator) {
    return 'platformOperator';
  }

  const role = session.account.role;

  if (requirement.roles !== undefined && !isTenantlessOperator && (role === null || !requirement.roles.includes(role))) {
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
