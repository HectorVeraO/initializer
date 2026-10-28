import type { MembershipRef, TenantRef, TenantRole } from '@/platform/core';

/** The signed-in account as the session reports it. */
export interface SessionAccount {
  readonly id: string;
  readonly email: string;
  readonly fullName: string | null;
  /** Role in the active tenant, null without one. */
  readonly role: TenantRole | null;
  readonly isPlatformOperator: boolean;
}

/** `data` of GET /auth/session, POST /auth/refresh and POST /auth/switch-tenant (identity `SessionOk`). */
export interface SessionView {
  readonly account: SessionAccount;
  readonly activeTenant: TenantRef | null;
  /** Active memberships only. */
  readonly memberships: readonly MembershipRef[];
  /** Snapshot for the reported tenant; `{}` without the feature-flags module. */
  readonly flags: Readonly<Record<string, boolean>>;
}

/** `data` of POST /auth/logout. */
export interface LogoutResult {
  readonly logoutUrl: string;
}
