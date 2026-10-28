import { Provider, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { ACCESS_PROVIDER, FLAG_SOURCE, TENANT_SELECTION } from '@/platform/core';
import type { AccessContext, AccessProvider, FlagSource, SignInOptions } from '@/platform/core';

/** What a spec may replace on the fake access provider; every member defaults to a resolved spy. */
export interface FakeAccessOverrides {
  readonly switchTenant?: (tenantId: string) => Promise<void>;
  readonly reload?: () => Promise<void>;
  readonly signOut?: () => Promise<void>;
  readonly signIn?: (options: SignInOptions) => Promise<void>;
  /** Flags the fake `FLAG_SOURCE` enables; every other key is false. */
  readonly flags?: Readonly<Record<string, boolean>>;
}

/** Synthetic `AccessContext`: Jane Doe, admin of `tenant-a`. Override any top-level part. */
export function accessContext(overrides: Partial<AccessContext> = {}): AccessContext {
  const tenant = { id: '11111111-1111-4111-8111-111111111111', slug: 'tenant-a', name: 'Tenant A', status: 'active' } as const;

  const context: AccessContext = {
    account: { id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', fullName: 'Jane Doe' },
    isPlatformOperator: false,
    activeTenant: tenant,
    role: 'admin',
    memberships: [{ id: '33333333-3333-4333-8333-333333333333', tenant, role: 'admin', status: 'active' }],
    ...overrides
  };

  return context;
}

/**
 * Boundary fake of the access and flag ports. A `null` context is an anonymous visitor: not admitted, and `admit()`
 * answers a redirect to `/login`. Any other context is admitted.
 */
export function provideFakeAccess(context: AccessContext | null, overrides: FakeAccessOverrides = {}): Provider[] {
  const flags = overrides.flags ?? {};

  const flagSource: FlagSource = { isEnabled: (flagKey) => flags[flagKey] === true };

  const access = (): AccessProvider => {
    const router = inject(Router);

    const current = signal(context);

    const provider: AccessProvider = {
      admitted: computed(() => current() !== null),
      context: current.asReadonly(),
      admit: () => Promise.resolve(context === null ? router.createUrlTree(['/login']) : true),
      switchTenant: overrides.switchTenant ?? vi.fn(() => Promise.resolve()),
      reload: overrides.reload ?? vi.fn(() => Promise.resolve()),
      signOut: overrides.signOut ?? vi.fn(() => Promise.resolve()),
      signIn: overrides.signIn ?? vi.fn(() => Promise.resolve())
    };

    return provider;
  };

  const providers: Provider[] = [
    { provide: ACCESS_PROVIDER, useFactory: access },
    { provide: FLAG_SOURCE, useValue: flagSource }
  ];

  return providers;
}

/** Binds `TENANT_SELECTION` to a console whose no-tenant page is `path`; `null` is a single-tenant console. */
export function provideTenantSelection(path: string | null): Provider {
  const provider: Provider = { provide: TENANT_SELECTION, useValue: path === null ? null : { noTenantPath: path } };

  return provider;
}
