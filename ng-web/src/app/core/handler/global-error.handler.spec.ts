import { TestBed } from '@angular/core/testing';
import { MessageService } from '@openng/optimus-ui/api';
import { GlobalErrorHandler } from './global-error.handler';

describe('GlobalErrorHandler', () => {
  const add = vi.fn();

  let handler: GlobalErrorHandler;

  let consoleError: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    add.mockClear();
    consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    TestBed.configureTestingModule({ providers: [GlobalErrorHandler, { provide: MessageService, useValue: { add } }] });
    handler = TestBed.inject(GlobalErrorHandler);
  });

  afterEach(() => {
    consoleError.mockRestore();
  });

  it('shows the notification an error carries as a toast', () => {
    handler.handleError({ notification: { severity: 'error', summary: 'S', detail: 'D' } });

    expect(add).toHaveBeenCalledTimes(1);
    expect(add).toHaveBeenCalledWith({ severity: 'error', summary: 'S', detail: 'D' });
  });

  it('shows no toast for an error without a notification', () => {
    handler.handleError(new Error('boom'));

    expect(add).not.toHaveBeenCalled();
  });
});
