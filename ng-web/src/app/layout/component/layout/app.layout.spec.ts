import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { TranslocoTestingModule } from '@jsverse/transloco';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ConfirmationService, MessageService } from '@openng/optimus-ui/api';
import { coreModule, providePlatform } from '@/platform/core';
import { provideOptimus } from '@openng/optimus-ui/config';
import es from '../../../../assets/i18n/es.json';
import { BRAND } from '@/core/config/brand';
import { ConfirmDeleteService } from '@/core/ui/confirm-delete/confirm-delete.service';
import { byTestId, deleteConfirmAccept, settle, stubMatchMedia, textOf, typeInto } from '@/core/testing/ui-testing';
import { AppLayout } from './app.layout';

describe('AppLayout', () => {
  beforeEach(() => {
    stubMatchMedia();
    TestBed.configureTestingModule({
      imports: [TranslocoTestingModule.forRoot({ langs: { es }, translocoConfig: { availableLangs: ['es'], defaultLang: 'es' }, preloadLangs: true })],
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting(), providePlatform([coreModule]), { provide: MessageService, useValue: { add: vi.fn() } }, provideOptimus({}), ConfirmationService]
    });
  });

  afterEach(() => {
    document.body.querySelectorAll('.p-dialog-mask').forEach((node) => node.remove());
  });

  function confirmButton(label: string): HTMLButtonElement {
    const buttons = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="alertdialog"] button'));

    return buttons.find((button) => textOf(button) === label) as HTMLButtonElement;
  }

  it('renders the shell: topbar with the text logo, sidebar menu and footer', () => {
    const fixture = TestBed.createComponent(AppLayout);

    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;

    expect(textOf(byTestId(root, 'topbar-logo'))).toBe(BRAND);
    expect(root.querySelector('.layout-sidebar .layout-menu')).not.toBeNull();
    expect(textOf(byTestId(root, 'menu-main'))).toContain('Inicio');
    expect(root.querySelector('.layout-footer')).not.toBeNull();
    expect(root.querySelector('router-outlet')).not.toBeNull();
  });

  it('renders the simple confirmation with its message and runs accept or reject', async () => {
    const fixture = TestBed.createComponent(AppLayout);

    fixture.detectChanges();

    const accepted = vi.fn();

    const rejected = vi.fn();

    TestBed.inject(ConfirmationService).confirm({ message: 'Este cambio es delicado', acceptLabel: 'Aceptar', rejectLabel: 'Cancelar', accept: accepted, reject: rejected });
    await settle(fixture);

    expect(textOf(byTestId(document.body, 'confirm-message'))).toBe('Este cambio es delicado');

    confirmButton('Cancelar').click();
    await settle(fixture);

    expect(rejected).toHaveBeenCalledTimes(1);
    expect(accepted).not.toHaveBeenCalled();

    TestBed.inject(ConfirmationService).confirm({ message: 'Otra vez', acceptLabel: 'Aceptar', rejectLabel: 'Cancelar', accept: accepted, reject: rejected });
    await settle(fixture);
    confirmButton('Aceptar').click();
    await settle(fixture);

    expect(accepted).toHaveBeenCalledTimes(1);
  });

  it('enables accept of the typed-keyword delete dialog only with the right keyword', async () => {
    const fixture = TestBed.createComponent(AppLayout);

    fixture.detectChanges();

    TestBed.inject(ConfirmDeleteService).confirm({ acceptLabel: 'Eliminar' });
    await settle(fixture);

    const input = document.body.querySelector('.confirm-delete-dialog input') as HTMLInputElement;

    typeInto(input, 'otra cosa');
    await settle(fixture);

    expect(deleteConfirmAccept().disabled).toBe(true);

    typeInto(input, 'eliminar');
    await settle(fixture);

    expect(deleteConfirmAccept().disabled).toBe(false);
  });
});
