import { TestBed } from '@angular/core/testing';
import { provideRouter, Routes } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../../../assets/i18n/es.json';
import { environment } from '../../../../environments/environment';
import { buildAppRoutes } from '../../../../app.routes';
import { DEV_ROUTES } from '@/dev/dev.routes.prod';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { GatbaseWebModule, SessionStore, coreModule, provideGatbase } from '@/gatbase/core';
import { errorEnvelope } from '../../../../testing/fixtures/api-envelope';
import { okEnvelope, sessionView } from '../../../../testing/fixtures/session-view';
import { AppMenu } from './app.menu';

function routePaths(routes: Routes, prefix = ''): string[] {
  return routes.flatMap((route) => {
    const own = [prefix, route.path ?? ''].filter(Boolean).join('/');

    return [own, ...routePaths(route.children ?? [], own)];
  });
}

describe('AppMenu', () => {
  const originalProduction = environment.production;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting(), provideGatbase([coreModule]), { provide: MessageService, useValue: { add: vi.fn() } }]
    });
  });

  afterEach(() => {
    environment.production = originalProduction;
  });

  function render(production: boolean): HTMLElement {
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

  describe('Gatbase sections', () => {
    const reportsModule: GatbaseWebModule = {
      id: 'reports',
      shellRoutes: [],
      publicRoutes: [],
      i18n: { scope: 'gatbase-reports', alias: 'gatbaseReports', loader: { es: () => Promise.resolve({ menu: { title: 'Informes' }, nav: { members: 'Miembros', admins: 'Solo admins' } }) } },
      nav: [
        { id: 'members', labelKey: 'nav.members', icon: 'menu-icon menu-icon-users', path: '/members', requires: { tenant: 'required' } },
        { id: 'admins', labelKey: 'nav.admins', icon: 'menu-icon menu-icon-cog', path: '/admins', requires: { roles: ['admin'] } }
      ]
    };

    beforeEach(() => {
      TestBed.configureTestingModule({ providers: [provideGatbase([coreModule, reportsModule])] });
    });

    afterEach(() => {
      TestBed.inject(HttpTestingController).verify();
    });

    async function renderWithSession(status: number, view: object): Promise<HTMLElement> {
      const http = TestBed.inject(HttpTestingController);

      const loaded = TestBed.inject(SessionStore).ensureLoaded();

      await vi.waitFor(() => http.expectOne('/assets/config/runtime-config.json').flush({ apiUrl: '' }));
      await vi.waitFor(() => http.expectOne('/api/v1/auth/session').flush(view, { status, statusText: 'x' }));
      await loaded;
      environment.production = true;

      const fixture = TestBed.createComponent(AppMenu);

      fixture.detectChanges();
      await vi.waitFor(() => {
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).not.toContain('nav.');
      });

      return fixture.nativeElement as HTMLElement;
    }

    function labelOf(root: HTMLElement, testId: string): string | undefined {
      return root.querySelector(`[data-testid="${testId}"]`)?.textContent?.trim();
    }

    it('lists the entries the session meets, labelled through the module scope', async () => {
      const root = await renderWithSession(200, okEnvelope(sessionView()));

      expect(labelOf(root, 'nav-members')).toBe('Miembros');
      expect(labelOf(root, 'nav-admins')).toBe('Solo admins');
    });

    it('hides an entry whose role requirement the session misses', async () => {
      const account = { id: 'a', email: 'viewer@example.com', fullName: null, role: 'viewer', isPlatformOperator: false };

      const root = await renderWithSession(200, okEnvelope(sessionView({ account })));

      expect(labelOf(root, 'nav-members')).toBe('Miembros');
      expect(root.querySelector('[data-testid="nav-admins"]')).toBeNull();
    });

    it('titles the module section through the module scope', async () => {
      const root = await renderWithSession(200, okEnvelope(sessionView()));

      expect(labelOf(root, 'menu-module-reports')).toContain('Informes');
    });

    it('shows no Gatbase section while anonymous', async () => {
      const root = await renderWithSession(401, errorEnvelope('SESSION_INVALID'));

      expect(root.querySelector('[data-testid="menu-module-reports"]')).toBeNull();
      expect(root.querySelector('[data-testid="nav-members"]')).toBeNull();
    });

    it('shows no section for a module whose entries the session misses entirely', async () => {
      const root = await renderWithSession(200, okEnvelope(sessionView({ activeTenant: null, account: { id: 'a', email: 'op@example.com', fullName: null, role: null, isPlatformOperator: true } })));

      expect(root.querySelector('[data-testid="menu-module-reports"]')).toBeNull();
    });
  });
});
