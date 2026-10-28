import { Provider, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Routes } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../../../assets/i18n/es.json';
import { environment } from '../../../../environments/environment';
import { buildAppRoutes } from '../../../../app.routes';
import { DEV_ROUTES } from '@/dev/dev.routes.prod';
import { byTestId, textOf } from '@/core/testing/ui-testing';
import { MessageService } from '@openng/optimus-ui/api';
import { PLATFORM_MODULES, coreModule } from '@/platform/core';
import type { AccessContext, WebModule } from '@/platform/core';
import { accessContext, provideFakeAccess, provideTenantSelection } from '../../../../testing/fixtures/access-testing';
import { AppMenu } from './app.menu';

function routePaths(routes: Routes, prefix = ''): string[] {
  return routes.flatMap((route) => {
    const own = [prefix, route.path ?? ''].filter(Boolean).join('/');

    return [own, ...routePaths(route.children ?? [], own)];
  });
}

/** The mandatory first module, contributing nothing. */
const STUB_CORE: WebModule = { id: 'core', shellRoutes: [], publicRoutes: [], nav: [], i18n: { scope: 'core', alias: 'core', loader: { es: () => Promise.resolve({}) } } };

describe('AppMenu', () => {
  const originalProduction = environment.production;

  afterEach(() => {
    environment.production = originalProduction;
  });

  function configure(modules: readonly WebModule[], providers: Provider[]): void {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), { provide: PLATFORM_MODULES, useValue: modules }, { provide: MessageService, useValue: { add: vi.fn() } }, ...providers]
    });
  }

  function render(production: boolean): HTMLElement {
    configure([STUB_CORE], []);
    environment.production = production;

    const fixture = TestBed.createComponent(AppMenu);

    fixture.detectChanges();

    return fixture.nativeElement as HTMLElement;
  }

  it('shows the home entry', () => {
    const root = render(true);

    const labels = Array.from(root.querySelectorAll('[data-testid="menu-main"] .layout-submenu-container .layout-menuitem-text')).map((label) => label.textContent?.trim());

    expect(labels).toEqual(['Inicio']);
  });

  it('adds the developer guide section outside production', () => {
    const root = render(false);

    expect(root.querySelector('[data-testid="menu-developer"]')).not.toBeNull();
  });

  it('has no developer guide section in production', () => {
    const root = render(true);

    expect(root.querySelector('[data-testid="menu-developer"]')).toBeNull();
  });

  it('registers no /dev routes in the production route table', () => {
    const paths = routePaths(buildAppRoutes(DEV_ROUTES, [coreModule]));

    expect(paths).toContain('home');
    expect(paths.filter((path) => path.startsWith('dev/'))).toEqual([]);
  });

  describe('Module sections', () => {
    const reportsModule: WebModule = {
      id: 'reports',
      shellRoutes: [],
      publicRoutes: [],
      i18n: { scope: 'reports', alias: 'reports', loader: { es: () => Promise.resolve({ menu: { title: 'Informes' }, nav: { open: 'Abierto', members: 'Miembros', anyone: 'Cualquiera', admins: 'Solo admins' } }) } },
      nav: [
        { id: 'open', labelKey: 'nav.open', icon: 'menu-icon menu-icon-users', path: '/open', requires: {} },
        { id: 'members', labelKey: 'nav.members', icon: 'menu-icon menu-icon-users', path: '/members', requires: { tenant: 'required' } },
        { id: 'anyone', labelKey: 'nav.anyone', icon: 'menu-icon menu-icon-users', path: '/anyone', requires: { tenant: 'required-unless-operator' } },
        { id: 'admins', labelKey: 'nav.admins', icon: 'menu-icon menu-icon-cog', path: '/admins', requires: { roles: ['admin'] } }
      ]
    };

    const MEMBER = accessContext({ role: 'member' });

    const TENANTLESS_MEMBER = accessContext({ role: null, activeTenant: null, memberships: [] });

    async function renderWith(providers: Provider[]): Promise<HTMLElement> {
      configure([STUB_CORE, reportsModule], providers);
      environment.production = true;

      const fixture = TestBed.createComponent(AppMenu);

      fixture.detectChanges();
      await vi.waitFor(() => {
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).not.toContain('nav.');
      });

      return fixture.nativeElement as HTMLElement;
    }

    function shownEntries(root: HTMLElement): string[] {
      const entries = Array.from(root.querySelectorAll('[data-testid^="nav-"]')).map((item) => (item.getAttribute('data-testid') ?? '').replace('nav-', ''));

      return entries;
    }

    describe('in a multi-tenant console', () => {
      const multiTenant = (context: AccessContext | null): Provider[] => [...provideFakeAccess(context), provideTenantSelection('/no-tenant')];

      it('lists the entries the visitor meets, labelled through the module scope', async () => {
        const root = await renderWith(multiTenant(accessContext()));

        expect(shownEntries(root)).toEqual(['open', 'members', 'anyone', 'admins']);
        expect(textOf(byTestId(root, 'nav-members'))).toBe('Miembros');
        expect(textOf(byTestId(root, 'nav-admins'))).toBe('Solo admins');
      });

      it('hides an entry whose role requirement the visitor misses', async () => {
        const root = await renderWith(multiTenant(MEMBER));

        expect(shownEntries(root)).toEqual(['open', 'members', 'anyone']);
      });

      it('hides the entries that need a tenant from a tenantless visitor', async () => {
        const root = await renderWith(multiTenant(TENANTLESS_MEMBER));

        expect(shownEntries(root)).toEqual(['open']);
      });

      it('shows a tenantless platform operator the entry that lets operators through', async () => {
        const root = await renderWith(multiTenant(accessContext({ role: null, activeTenant: null, memberships: [], isPlatformOperator: true })));

        expect(shownEntries(root)).toEqual(['open', 'anyone']);
      });

      it('titles the module section through the module scope', async () => {
        const root = await renderWith(multiTenant(accessContext()));

        expect(textOf(byTestId(root, 'menu-module-reports'))).toContain('Informes');
      });

      it('shows no module section while the visitor is not admitted', async () => {
        const root = await renderWith(multiTenant(null));

        expect(root.querySelector('[data-testid="menu-module-reports"]')).toBeNull();
        expect(shownEntries(root)).toEqual([]);
      });

      it('follows the visitor: the entries change when the context does', async () => {
        const visitor = signal<AccessContext | null>(MEMBER);

        const root = await renderWith([...provideFakeAccess(visitor), provideTenantSelection('/no-tenant')]);

        expect(shownEntries(root)).toEqual(['open', 'members', 'anyone']);
        visitor.set(accessContext());
        await vi.waitFor(() => expect(shownEntries(root)).toEqual(['open', 'members', 'anyone', 'admins']));
      });
    });

    describe('in a single-tenant console', () => {
      it('shows required-unless-operator to a tenantless member and hides required', async () => {
        const root = await renderWith([...provideFakeAccess(TENANTLESS_MEMBER), provideTenantSelection(null)]);

        expect(shownEntries(root)).toEqual(['open', 'anyone']);
      });
    });

    describe('under the default open access', () => {
      it('shows an entry without a clause and hides every entry with one', async () => {
        const root = await renderWith([]);

        expect(shownEntries(root)).toEqual(['open']);
      });
    });
  });
});
