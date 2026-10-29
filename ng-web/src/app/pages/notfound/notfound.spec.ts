import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { provideOptimus } from '@openng/optimus-ui/config';
import { BRAND } from '@/core/config/brand';
import { textOf } from '@/core/testing/ui-testing';
import es from '../../../assets/i18n/es.json';
import { Notfound } from './notfound';

describe('Notfound', () => {
  it('renders the brand above the 404 message', () => {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), provideOptimus({})]
    });
    const fixture = TestBed.createComponent(Notfound);

    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;

    expect(textOf(root)).toContain(BRAND);
    expect(textOf(root)).toContain('404');
  });
});
