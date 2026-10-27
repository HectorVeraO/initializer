import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../assets/i18n/es.json';
import { environment } from '../../environments/environment';
import { HomeComponent } from './home.component';

describe('HomeComponent', () => {
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

    const fixture = TestBed.createComponent(HomeComponent);

    fixture.detectChanges();

    return fixture.nativeElement as HTMLElement;
  }

  it('links the component demos outside production', () => {
    const root = render(false);

    expect(root.querySelector('[data-testid="home-demo-dashboard"]')?.getAttribute('href')).toBe('/dev/dashboard');
    expect(root.querySelector('[data-testid="home-demo-table"]')?.getAttribute('href')).toBe('/dev/uikit/table');
  });

  it('shows no demo links in production', () => {
    const root = render(true);

    expect(root.querySelector('[data-testid="home-intro"]')).not.toBeNull();
    expect(root.querySelectorAll('a')).toHaveLength(0);
  });
});
