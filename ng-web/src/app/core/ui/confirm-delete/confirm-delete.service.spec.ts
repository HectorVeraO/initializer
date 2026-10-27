import { TestBed } from '@angular/core/testing';
import { TranslocoTestingModule } from '@jsverse/transloco';
import es from '../../../../assets/i18n/es.json';
import { ConfirmDeleteService } from './confirm-delete.service';

describe('ConfirmDeleteService', () => {
  let service: ConfirmDeleteService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })]
    });
    service = TestBed.inject(ConfirmDeleteService);
  });

  function outcomesOf(options: Parameters<ConfirmDeleteService['confirm']>[0]): boolean[] {
    const outcomes: boolean[] = [];

    service.confirm(options).subscribe((outcome) => outcomes.push(outcome));

    return outcomes;
  }

  it('opens with the default texts and the keyword of the default accept label', () => {
    outcomesOf({});

    expect(service.visible()).toBe(true);
    expect(service.header()).toBe('Confirmar eliminación');
    expect(service.acceptLabel()).toBe('Eliminar');
    expect(service.confirmText()).toBe('eliminar');
  });

  it('derives the keyword from the first word of the accept label, lowercased', () => {
    outcomesOf({ acceptLabel: 'Desactivar tenant-a' });

    expect(service.confirmText()).toBe('desactivar');
  });

  it('keeps accented letters in the keyword and skips leading symbols', () => {
    outcomesOf({ acceptLabel: '1. Señalar ya' });

    expect(service.confirmText()).toBe('señalar');
  });

  it('falls back to the whole label when it has no letters', () => {
    outcomesOf({ acceptLabel: '123' });

    expect(service.confirmText()).toBe('123');
  });

  it('emits true and closes on accept', () => {
    const outcomes = outcomesOf({});

    service.accept();

    expect(outcomes).toEqual([true]);
    expect(service.visible()).toBe(false);
  });

  it('emits false and closes on reject', () => {
    const outcomes = outcomesOf({});

    service.reject();

    expect(outcomes).toEqual([false]);
    expect(service.visible()).toBe(false);
  });

  it('completes the observable after the outcome', () => {
    let completed = false;

    service.confirm({}).subscribe({ complete: () => (completed = true) });
    service.accept();

    expect(completed).toBe(true);
  });
});
