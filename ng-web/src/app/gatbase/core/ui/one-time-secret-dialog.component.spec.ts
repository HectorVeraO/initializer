import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { OneTimeSecretDialogComponent } from './one-time-secret-dialog.component';

const SECRET = 'synthetic-secret-value';

@Component({
  selector: 'app-host',
  imports: [OneTimeSecretDialogComponent],
  template: `<app-one-time-secret-dialog titleKey="gatbaseCore.invitations.linkTitle" [secret]="secret()" (closed)="secret.set(null)" />`
})
class HostComponent {
  readonly secret = signal<string | null>(null);
}

describe('OneTimeSecretDialogComponent', () => {
  beforeEach(() => {
    stubMatchMedia();
    configureFeatureTest();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  async function open() {
    const fixture = TestBed.createComponent(HostComponent);

    await settle(fixture);
    fixture.componentInstance.secret.set(SECRET);
    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(byTestId(document.body, 'one-time-secret-value')).not.toBeNull();
    });

    return fixture;
  }

  it('shows nothing without a secret', async () => {
    const fixture = TestBed.createComponent(HostComponent);

    await settle(fixture);

    expect(byTestId(document.body, 'one-time-secret-value')).toBeNull();
  });

  it('shows the value in a read-only field with the shown-once notice', async () => {
    await open();

    const value = byTestId(document.body, 'one-time-secret-value') as HTMLInputElement;

    expect(value.value).toBe(SECRET);
    expect(value.readOnly).toBe(true);
    await vi.waitFor(() => expect(textOf(document.body)).toContain('Este valor se muestra una sola vez'));
  });

  it('copies the value to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);

    vi.stubGlobal('navigator', { clipboard: { writeText } });
    await open();
    (byTestId(document.body, 'one-time-secret-copy') as HTMLButtonElement).click();

    expect(writeText).toHaveBeenCalledWith(SECRET);
  });

  it('emits closed and, once the host clears the value, the secret leaves the DOM', async () => {
    const fixture = await open();

    (byTestId(document.body, 'one-time-secret-close') as HTMLButtonElement).click();
    await settle(fixture);

    expect(fixture.componentInstance.secret()).toBeNull();
    expect(document.body.innerHTML).not.toContain(SECRET);
  });
});
