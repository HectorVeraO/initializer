/** Tenant roles of core's authorization surface. */
export type TenantRole = 'admin' | 'member' | 'viewer';
export type TenantStatus = 'active' | 'suspended';
export type MembershipStatus = 'active' | 'suspended' | 'revoked';

export interface TenantRef {
  readonly id: string;
  readonly slug: string;
  readonly name: string;
  readonly status: TenantStatus;
}

/** One membership as the session reports it. */
export interface MembershipRef {
  readonly id: string;
  readonly tenant: TenantRef;
  readonly role: TenantRole;
  readonly status: MembershipStatus;
}

export interface AccountRef {
  readonly id: string;
  readonly email: string;
  readonly fullName: string | null;
}

/** What the shell knows about a signed-in visitor. Tenant fields are null/empty without tenancy. */
export interface AccessContext {
  readonly account: AccountRef;
  readonly isPlatformOperator: boolean;
  readonly activeTenant: TenantRef | null;
  /** Role in the active tenant; null without one. */
  readonly role: TenantRole | null;
  /** Active memberships, as the session reports them; empty without tenancy. */
  readonly memberships: readonly MembershipRef[];
}

/** What a route or nav entry requires. `{}`: any visitor the installed provider admits. */
export interface AccessRequirement {
  readonly tenant?: 'required' | 'required-unless-operator';
  readonly roles?: readonly TenantRole[];
  readonly platformOperator?: true;
  readonly flag?: string;
  /**
   * Web mirror of Go Policy.TenantlessOperator: a platform operator with no active tenant meets the `tenant`
   * and `roles` clauses. A visitor with an active tenant is still held to `roles`.
   * Valid only together with `roles` and without `platformOperator`.
   */
  readonly orTenantlessOperator?: true;
}

/** Closed set of notices a module may ask the sign-in page to show; the texts live in identity's scope (`login.notices.*`). */
export type SignInNotice = 'accountCreated';

export interface SignInOptions {
  readonly returnUrl?: string;
  readonly email?: string;
  readonly notice?: SignInNotice;
}
