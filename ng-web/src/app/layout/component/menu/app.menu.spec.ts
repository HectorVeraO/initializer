import { TestBed } from '@angular/core/testing';
import { provideRouter, Routes } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../../../assets/i18n/es.json';
import { environment } from '../../../../environments/environment';
import { buildAppRoutes } from '../../../../app.routes';
import { DEV_ROUTES } from '@/dev/dev.routes.prod';
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
      providers: [provideRouter([])]
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
    const paths = routePaths(buildAppRoutes(DEV_ROUTES));

    expect(paths).toContain('home');
    expect(paths.filter((path) => path.startsWith('dev/'))).toEqual([]);
  });
});
