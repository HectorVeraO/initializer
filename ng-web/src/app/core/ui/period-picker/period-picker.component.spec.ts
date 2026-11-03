import { Component } from '@angular/core';
import { ComponentFixture } from '@angular/core/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { PeriodSelection } from '@/core/period/period';
import { byTestId, configureFeatureTest, settle, stubMatchMedia, textOf } from '@/core/testing/ui-testing';
import { PeriodPickerComponent } from './period-picker.component';

@Component({
  selector: 'p-picker-host',
  standalone: true,
  imports: [PeriodPickerComponent],
  template: '<p-period-picker [defaultDays]="days" [optional]="optional" (periodChange)="seen.push($event)" />'
})
class HostComponent {
  days = 7;
  optional = false;
  seen: PeriodSelection[] = [];
}

@Component({
  selector: 'p-unsynced-picker-host',
  standalone: true,
  imports: [PeriodPickerComponent],
  template: '<p-period-picker [syncUrl]="false" (periodChange)="seen.push($event)" />'
})
class UnsyncedHostComponent {
  seen: PeriodSelection[] = [];
}

describe('PeriodPickerComponent', () => {
  interface Opened {
    fixture: ComponentFixture<unknown>;
    host: HostComponent;
  }

  async function open(url: string): Promise<Opened> {
    configureFeatureTest([{ path: 'view', component: HostComponent }]);

    const harness = await RouterTestingHarness.create();

    const host = await harness.navigateByUrl(url, HostComponent);

    await settle(harness.fixture);

    return { fixture: harness.fixture, host };
  }

  /** Opens the preset dropdown and clicks the option, as a person would. */
  async function select(fixture: ComponentFixture<unknown>, value: string): Promise<void> {
    stubMatchMedia();
    byTestId(fixture.nativeElement as HTMLElement, 'period-preset')?.click();
    await settle(fixture);

    const option = byTestId(document.body, `period-option-${value}`);

    expect(option).not.toBeNull();
    option?.click();
    await settle(fixture);
  }

  function customInput(fixture: ComponentFixture<unknown>): HTMLInputElement | null {
    return (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>('#period-custom-input');
  }

  afterEach(() => {
    document.body.querySelectorAll('.p-select-overlay, .p-datepicker-panel').forEach((node) => node.remove());
  });

  it('opens on the last 7 days and labels that range with Santiago days, without the time', async () => {
    const { fixture, host } = await open('/view');

    const first = host.seen[0];

    expect(first?.preset).toBe('default');

    const days = (new Date(first?.period?.to ?? '').getTime() - new Date(first?.period?.from ?? '').getTime()) / 86_400_000;

    expect(days).toBe(7);
    expect(textOf(byTestId(fixture.nativeElement as HTMLElement, 'period-label'))).toMatch(/^Rango: \d{2}\/\d{2}\/\d{4} - \d{2}\/\d{2}\/\d{4}$/);
  });

  it('opens exactly on the range of the URL, shown in the custom picker and not repeated beside it', async () => {
    const { fixture, host } = await open('/view?from=2026-10-01T03:00:00Z&to=2026-10-04T03:00:00Z');

    expect(host.seen[0]?.period).toEqual({ from: '2026-10-01T03:00:00.000Z', to: '2026-10-04T03:00:00.000Z' });
    expect(customInput(fixture)?.value).toBe('01/10/2026 - 03/10/2026');
    expect(byTestId(fixture.nativeElement as HTMLElement, 'period-label')).toBeNull();
  });

  it('opens "Personalizado" filled with the range in force, and reports nothing until a range is picked', async () => {
    const { fixture, host } = await open('/view');

    const reported = host.seen.length;

    await select(fixture, 'custom');

    expect(customInput(fixture)?.value).toMatch(/^\d{2}\/\d{2}\/\d{4} - \d{2}\/\d{2}\/\d{4}$/);
    expect(host.seen).toHaveLength(reported);
    expect(byTestId(fixture.nativeElement as HTMLElement, 'period-label')).toBeNull();
  });

  it('shows the localized placeholder when "Personalizado" opens with no range in force', async () => {
    configureFeatureTest();

    const picker = TestBed.createComponent(PeriodPickerComponent);

    picker.componentRef.setInput('optional', true);
    picker.detectChanges();
    await select(picker, 'custom');

    expect(customInput(picker)?.value).toBe('');
    expect(customInput(picker)?.getAttribute('placeholder')).toBe('dd/mm/aaaa - dd/mm/aaaa');
  });

  it('ignores an invalid range in the URL and uses the default', async () => {
    const { host } = await open('/view?from=nope&to=2026-10-04T03:00:00Z');

    expect(host.seen[0]?.preset).toBe('default');
  });

  it('writes the chosen range to the URL and reports it', async () => {
    const { fixture, host } = await open('/view');

    await select(fixture, 'thirty');

    const last = host.seen.at(-1);

    expect(last?.preset).toBe('thirty');

    const url = TestBed.inject(Router).parseUrl(TestBed.inject(Router).url);

    expect(url.queryParams['from']).toBe(last?.period?.from);
    expect(url.queryParams['to']).toBe(last?.period?.to);
  });

  it('lists a duration once when the default days equal a preset', async () => {
    configureFeatureTest();

    const picker = TestBed.createComponent(PeriodPickerComponent);

    picker.componentRef.setInput('defaultDays', 30);
    picker.detectChanges();
    stubMatchMedia();
    byTestId(picker.nativeElement as HTMLElement, 'period-preset')?.click();
    await settle(picker);

    const labels = Array.from(document.body.querySelectorAll('[data-testid^="period-option-"]')).map((node) => node.textContent?.trim());

    expect(labels.filter((label) => label === 'Últimos 30 días')).toHaveLength(1);
    expect(labels).toContain('Últimos 90 días');
  });

  it('offers "no range" first and reports no period when the view is optional', async () => {
    configureFeatureTest([{ path: 'view', component: HostComponent }]);

    const picker = TestBed.createComponent(PeriodPickerComponent);

    picker.componentRef.setInput('optional', true);

    const seen: PeriodSelection[] = [];

    picker.componentInstance.periodChange.subscribe((value) => seen.push(value));
    picker.detectChanges();

    expect(seen).toEqual([{ preset: 'none', period: null }]);
    expect(byTestId(picker.nativeElement as HTMLElement, 'period-label')).toBeNull();
  });

  it('ignores the range of the URL and keeps its own range out of it when syncUrl is false', async () => {
    configureFeatureTest([{ path: 'view', component: UnsyncedHostComponent }]);

    const harness = await RouterTestingHarness.create();

    const host = await harness.navigateByUrl('/view?from=2026-10-01T03:00:00Z&to=2026-10-04T03:00:00Z', UnsyncedHostComponent);

    await settle(harness.fixture);

    expect(host.seen[0]?.preset).toBe('default');

    await select(harness.fixture, 'thirty');

    expect(host.seen.at(-1)?.preset).toBe('thirty');

    const url = TestBed.inject(Router).parseUrl(TestBed.inject(Router).url);

    expect(url.queryParams['from']).toBe('2026-10-01T03:00:00Z');
    expect(url.queryParams['to']).toBe('2026-10-04T03:00:00Z');
  });
});
