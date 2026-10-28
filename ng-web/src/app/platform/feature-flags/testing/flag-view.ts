/** Synthetic `FlagView` for specs; override any field. */
export function flagView(key: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key,
    type: 'ops',
    default: true,
    expiresAt: null,
    description: 'Synthetic flag',
    globalOverride: null,
    tenantOverrides: [],
    ...overrides
  };
}

/** Synthetic tenant override entry of a `FlagView`. */
export function tenantOverride(tenantId: string, enabled: boolean): Record<string, unknown> {
  return { tenantId, enabled, updatedAt: '2026-01-01T00:00:00Z' };
}

/** Synthetic `TenantView` for specs. */
export function tenantView(id: string, slug: string, name: string): Record<string, unknown> {
  return { id, slug, name, status: 'active', createdAt: '2026-01-01T00:00:00Z' };
}
