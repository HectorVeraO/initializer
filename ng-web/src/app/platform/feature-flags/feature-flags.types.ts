export type FlagType = 'release' | 'ops';

/** A flag with its global override and tenant overrides (feature-flags `FlagView`). */
export interface FlagView {
  readonly key: string;
  readonly type: FlagType;
  readonly default: boolean;
  /** `YYYY-MM-DD`; null for ops flags. */
  readonly expiresAt: string | null;
  readonly description: string;
  readonly globalOverride: { readonly enabled: boolean; readonly updatedAt: string } | null;
  readonly tenantOverrides: readonly { readonly tenantId: string; readonly enabled: boolean; readonly updatedAt: string }[];
}

export interface EnabledBody {
  readonly enabled: boolean;
}
