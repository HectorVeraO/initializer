import { inject } from '@angular/core';
import { CanMatchFn, Router, UrlTree } from '@angular/router';
import type { FlagSource } from '../flags/flag-source.port';
import { FLAG_SOURCE } from '../flags/flag-source.port';
import { TENANT_SELECTION } from '../tenants/tenant-selection.port';
import type { AccessContext, AccessRequirement } from './access.types';
import { ACCESS_PROVIDER } from './access.port';
import type { AccessProvider } from './access.port';

export type UnmetClause = 'tenant' | 'platformOperator' | 'roles' | 'flag';

const FORBIDDEN_PATH = '/forbidden';

/** Throws Error('PLATFORM_INVALID_REQUIREMENT') when orTenantlessOperator is set without roles, or together with platformOperator. */
export function assertValidRequirement(requirement: AccessRequirement): void {
  if (requirement.orTenantlessOperator !== true) {
    return;
  }

  const hasRoles = requirement.roles !== undefined;

  const hasPlatformOperator = requirement.platformOperator === true;

  if (!hasRoles || hasPlatformOperator) {
    throw new Error('PLATFORM_INVALID_REQUIREMENT');
  }
}

/** The first clause the requirement declares, in the order tenant, platformOperator, roles, flag. */
function firstDeclaredClause(requirement: AccessRequirement): UnmetClause | null {
  if (requirement.tenant !== undefined) {
    return 'tenant';
  }

  if (requirement.platformOperator === true) {
    return 'platformOperator';
  }

  if (requirement.roles !== undefined) {
    return 'roles';
  }

  if (requirement.flag !== undefined) {
    return 'flag';
  }

  return null;
}

/** Pure: first unmet clause in the order tenant, platformOperator, roles, flag; null when every clause is met. A null context fails every declared clause. */
export function firstUnmetClause(context: AccessContext | null, flags: FlagSource, multiTenant: boolean, requirement: AccessRequirement): UnmetClause | null {
  if (context === null) {
    return firstDeclaredClause(requirement);
  }

  const hasNoActiveTenant = context.activeTenant === null;

  const isTenantlessOperator = requirement.orTenantlessOperator === true && hasNoActiveTenant && context.isPlatformOperator;

  if (requirement.tenant === 'required' && hasNoActiveTenant && !isTenantlessOperator) {
    return 'tenant';
  }

  // Without tenancy nothing needs a tenant: the single-tenant console meets this clause.
  if (requirement.tenant === 'required-unless-operator' && hasNoActiveTenant && !context.isPlatformOperator && multiTenant) {
    return 'tenant';
  }

  if (requirement.platformOperator === true && !context.isPlatformOperator) {
    return 'platformOperator';
  }

  if (requirement.roles !== undefined && !isTenantlessOperator) {
    const role = context.role;

    const hasAllowedRole = role !== null && requirement.roles.includes(role);

    if (!hasAllowedRole) {
      return 'roles';
    }
  }

  if (requirement.flag !== undefined && !flags.isEnabled(requirement.flag)) {
    return 'flag';
  }

  return null;
}

/** Validated, detached copy: later changes to the caller's object or array cannot change what a guard enforces. */
function freezeRequirement(requirement: AccessRequirement): AccessRequirement {
  assertValidRequirement(requirement);

  const roles = requirement.roles === undefined ? undefined : Object.freeze([...requirement.roles]);

  const copy: AccessRequirement = Object.freeze({ ...requirement, ...(roles !== undefined && { roles }) });

  return copy;
}

/**
 * Guard factory; throws on an invalid requirement at construction. On match: ACCESS_PROVIDER.admit() (a UrlTree is returned as is),
 * then firstUnmetClause with multiTenant = TENANT_SELECTION !== null:
 *   tenant           -> TENANT_SELECTION.noTenantPath, or /forbidden when TENANT_SELECTION is null
 *   platformOperator -> /forbidden
 *   roles            -> /forbidden
 *   flag             -> false (the path looks missing)
 *   none             -> true
 * Navigation UX only: the backend enforces every rule.
 */
export function requireAccess(requirement: AccessRequirement): CanMatchFn {
  const enforced = freezeRequirement(requirement);

  return async (): Promise<boolean | UrlTree> => {
    const provider = inject(ACCESS_PROVIDER);

    const flags = inject(FLAG_SOURCE);

    const selection = inject(TENANT_SELECTION);

    const router = inject(Router);

    const admission = await provider.admit();

    if (admission !== true) {
      return admission;
    }

    const unmet = firstUnmetClause(provider.context(), flags, selection !== null, enforced);

    switch (unmet) {
      case 'tenant':
        return router.createUrlTree([selection?.noTenantPath ?? FORBIDDEN_PATH]);
      case 'platformOperator':
      case 'roles':
        return router.createUrlTree([FORBIDDEN_PATH]);
      case 'flag':
        return false;
      default:
        return true;
    }
  };
}

/** Synchronous visibility for the menu and the no-tenant page: the provider admits the visitor and no clause is unmet. */
export function allowsAccess(provider: AccessProvider, flags: FlagSource, multiTenant: boolean, requirement: AccessRequirement): boolean {
  const isAdmitted = provider.admitted();

  const isAllowed = isAdmitted && firstUnmetClause(provider.context(), flags, multiTenant, requirement) === null;

  return isAllowed;
}
