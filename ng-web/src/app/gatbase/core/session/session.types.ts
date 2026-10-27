export type MembershipRole = 'admin' | 'member' | 'viewer';
export type TenantStatus = 'active' | 'suspended';
export type MembershipStatus = 'active' | 'suspended' | 'revoked';

export interface TenantSummary {
  readonly id: string;
  readonly slug: string;
  readonly name: string;
  readonly status: TenantStatus;
}

/** The one membership item shape. */
export interface SessionMembership {
  readonly id: string;
  readonly tenant: TenantSummary;
  readonly role: MembershipRole;
  readonly status: MembershipStatus;
}

export interface SessionAccount {
  readonly id: string;
  readonly email: string;
  readonly fullName: string | null;
  /** Role in the active tenant, null without one. */
  readonly role: MembershipRole | null;
  readonly isPlatformOperator: boolean;
}

/** `data` of GET /auth/session, POST /auth/refresh and POST /auth/switch-tenant (core-auth `SessionOk`). */
export interface SessionView {
  readonly account: SessionAccount;
  readonly activeTenant: TenantSummary | null;
  /** Active memberships only. */
  readonly memberships: readonly SessionMembership[];
  /** Snapshot for the reported tenant; `{}` without the feature-flags module. */
  readonly flags: Readonly<Record<string, boolean>>;
}

/** `data` of POST /auth/logout. */
export interface LogoutResult {
  readonly logoutUrl: string;
}
