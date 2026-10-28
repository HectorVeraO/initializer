import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { MessageService } from '@openng/optimus-ui/api';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../testing/fixtures/access-testing';
import type { AccessContext } from './access/access.types';
import { coreModule } from './module';

describe('coreModule api-keys route', () => {
  let router: Router;

  async function go(context: AccessContext | null): Promise<string> {
    const apiKeysRoute = coreModule.shellRoutes.find((route) => route.path === 'api-keys');

    configureFeatureTest(
      [
        { path: 'api-keys', canMatch: apiKeysRoute?.canMatch, component: BlankComponent },
        { path: 'forbidden', component: BlankComponent },
        { path: 'no-tenant', component: BlankComponent },
        { path: 'login', component: BlankComponent }
      ],
      [...provideFakeAccess(context), provideTenantSelection('/no-tenant'), { provide: MessageService, useValue: { add: vi.fn() } }]
    );
    router = TestBed.inject(Router);
    await router.navigateByUrl('/api-keys');

    return router.url;
  }

  it('opens for an admin of the active tenant', async () => {
    expect(await go(accessContext())).toBe('/api-keys');
  });

  it('sends a member to /forbidden', async () => {
    expect(await go(accessContext({ role: 'member' }))).toBe('/forbidden');
  });

  it('keeps a tenantless platform operator out: the keys screen still needs a tenant', async () => {
    expect(await go(accessContext({ activeTenant: null, role: null, isPlatformOperator: true, memberships: [] }))).toBe('/no-tenant');
  });
});

describe('coreModule contributions', () => {
  it('puts the tenant switcher in the context slot and the account menu in the account slot', () => {
    const slots = (coreModule.topbar ?? []).map((item) => `${item.id}:${item.slot}`);

    expect(slots).toEqual(['tenant-switcher:context', 'account-menu:account']);
  });
});
