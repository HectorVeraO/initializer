import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { byTestId, configureFeatureTest, settle, textOf } from '@/core/testing/ui-testing';
import { InlineConfirmComponent } from './inline-confirm.component';

@Component({
  selector: 'app-host',
  imports: [InlineConfirmComponent],
  template: `<app-inline-confirm labelKey="gatbaseCore.invitations.revoke" testId="thing-revoke" [disabled]="isDisabled()" (confirmed)="count.set(count() + 1)" />`
})
class HostComponent {
  readonly count = signal(0);
  readonly isDisabled = signal(false);
}

describe('InlineConfirmComponent', () => {
  beforeEach(() => {
    configureFeatureTest();
  });

  async function render() {
    const fixture = TestBed.createComponent(HostComponent);

    await settle(fixture);
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(textOf(fixture.nativeElement)).toContain('Revocar');
    });

    return fixture;
  }

  function click(root: HTMLElement, testId: string): void {
    (byTestId(root, testId) as HTMLButtonElement).click();
  }

  it('turns into confirm and cancel in place and emits once on confirm', async () => {
    const fixture = await render();

    const root = fixture.nativeElement as HTMLElement;

    click(root, 'thing-revoke');
    await settle(fixture);
    expect(byTestId(root, 'thing-revoke')).toBeNull();
    expect(fixture.componentInstance.count()).toBe(0);

    click(root, 'thing-revoke-confirm');
    await settle(fixture);

    expect(fixture.componentInstance.count()).toBe(1);
    expect(byTestId(root, 'thing-revoke')).not.toBeNull();
    expect(byTestId(root, 'thing-revoke-confirm')).toBeNull();
  });

  it('emits nothing on cancel and goes back to the first button', async () => {
    const fixture = await render();

    const root = fixture.nativeElement as HTMLElement;

    click(root, 'thing-revoke');
    await settle(fixture);
    click(root, 'thing-revoke-cancel');
    await settle(fixture);

    expect(fixture.componentInstance.count()).toBe(0);
    expect(byTestId(root, 'thing-revoke')).not.toBeNull();
  });

  it('does not start a confirmation while disabled', async () => {
    const fixture = await render();

    const root = fixture.nativeElement as HTMLElement;

    fixture.componentInstance.isDisabled.set(true);
    await settle(fixture);
    click(root, 'thing-revoke');
    await settle(fixture);

    expect((byTestId(root, 'thing-revoke') as HTMLButtonElement).disabled).toBe(true);
    expect(byTestId(root, 'thing-revoke-confirm')).toBeNull();
    expect(fixture.componentInstance.count()).toBe(0);
  });
});
