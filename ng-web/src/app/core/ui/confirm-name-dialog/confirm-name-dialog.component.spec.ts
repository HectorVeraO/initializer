import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { provideOptimus } from '@openng/optimus-ui/config';
import es from '../../../../assets/i18n/es.json';
import { settle, stubMatchMedia } from '@/core/testing/ui-testing';
import { ConfirmDeleteService } from '../confirm-delete/confirm-delete.service';
import { ConfirmNameDialogComponent } from './confirm-name-dialog.component';

describe('ConfirmNameDialogComponent', () => {
  beforeEach(() => {
    stubMatchMedia();
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), provideOptimus({})]
    });
  });

  afterEach(() => {
    document.body.querySelectorAll('.p-dialog-mask').forEach((node) => node.remove());
  });

  it('shows a message containing markup as plain text', async () => {
    const fixture = TestBed.createComponent(ConfirmNameDialogComponent);

    fixture.detectChanges();

    const literal = '<img src=x onerror="alert(1)">';

    TestBed.inject(ConfirmDeleteService).confirm({ message: literal });
    await settle(fixture);

    const message = document.body.querySelector('.confirm-delete-message') as HTMLElement;

    expect(message.textContent).toBe(literal);
    expect(message.querySelector('img')).toBeNull();
  });
});
