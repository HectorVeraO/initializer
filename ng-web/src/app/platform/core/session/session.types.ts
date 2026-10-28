import type { MembershipStatus, TenantRef, TenantRole } from '../access/access.types';

export type { MembershipStatus, TenantStatus } from '../access/access.types';

export type MembershipRole = TenantRole;

/** One invitation as the API lists it (`InvitationView`). The token never appears here. */
export interface InvitationView {
  readonly id: string;
  readonly email: string;
  readonly role: MembershipRole;
  readonly status: 'pending' | 'accepted' | 'revoked' | 'expired';
  readonly expiresAt: string;
  readonly createdAt: string;
  readonly acceptedAt: string | null;
  readonly revokedAt: string | null;
}

export type TenantSummary = TenantRef;

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

/** `data` of GET /auth/session, POST /auth/refresh and POST /auth/switch-tenant (identity `SessionOk`). */
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
