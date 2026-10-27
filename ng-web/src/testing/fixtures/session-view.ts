/** Synthetic `SessionView` (the `data` of `SessionOk`) for specs; override any top-level part. */
export function sessionView(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  const tenant = { id: '11111111-1111-4111-8111-111111111111', slug: 'tenant-a', name: 'Tenant A', status: 'active' };

  return {
    account: { id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', fullName: 'Jane Doe', role: 'admin', isPlatformOperator: false },
    activeTenant: tenant,
    memberships: [{ id: '33333333-3333-4333-8333-333333333333', tenant, role: 'admin', status: 'active' }],
    flags: {},
    ...overrides
  };
}

/** Success envelope around `data`. */
export function okEnvelope(data: unknown): object {
  return { data, meta: { traceId: 'abc', timestamp: '2026-01-01T00:00:00Z' }, error: null };
}
