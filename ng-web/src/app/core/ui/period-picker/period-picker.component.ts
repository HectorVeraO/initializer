import { ChangeDetectionStrategy, Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { TranslocoModule, TranslocoService } from '@jsverse/transloco';
import { DatePickerModule } from '@openng/optimus-ui/datepicker';
import { SelectModule } from '@openng/optimus-ui/select';
import { formatSantiagoDay } from '@/core/format/santiago-date';
import { santiagoDateOf } from '@/core/format/santiago-time';
import { DEFAULT_PERIOD_DAYS, Period, PeriodPreset, PeriodSelection, customPeriod, parsePeriod, rollingPeriod } from '@/core/period/period';

const THIRTY_DAYS = 30;

const NINETY_DAYS = 90;

interface PresetOption {
  label: string;
  value: PeriodPreset;
}

/**
 * Period selector shared by the report views: "Ultimos N dias", 30, 90 or a
 * custom range of Santiago days. The range lives in the URL as `?from&to`.
 * Without `syncUrl` (the dashboard) it keeps the range to itself. A preset
 * shows its range as Santiago days next to the selector; the custom range
 * opens on the range in force and applies once both days are chosen.
 */
@Component({
  selector: 'p-period-picker',
  standalone: true,
  imports: [FormsModule, TranslocoModule, DatePickerModule, SelectModule],
  templateUrl: './period-picker.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class PeriodPickerComponent implements OnInit {
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly transloco = inject(TranslocoService);
  private readonly translation = toSignal(this.transloco.selectTranslation());

  readonly defaultDays = input(DEFAULT_PERIOD_DAYS);
  readonly optional = input(false);
  readonly syncUrl = input(true);
  readonly emitInitial = input(true);

  readonly periodChange = output<PeriodSelection>();

  readonly preset = signal<PeriodPreset>('default');
  readonly period = signal<Period | null>(null);
  readonly customRange = signal<(Date | null)[] | null>(null);

  readonly presetOptions = computed<PresetOption[]>(() => {
    this.translation();

    const lastDays = (days: number): string => this.transloco.translate('period.lastDays', { days });

    const candidates: PresetOption[] = [
      { label: lastDays(this.defaultDays()), value: 'default' },
      { label: lastDays(THIRTY_DAYS), value: 'thirty' },
      { label: lastDays(NINETY_DAYS), value: 'ninety' },
      { label: this.transloco.translate('period.custom'), value: 'custom' }
    ];

    // Presets with the same label (default days equal to 30 or 90) collapse to one entry; the selected one wins.
    const options: PresetOption[] = [];

    for (const candidate of candidates) {
      const twinIndex = options.findIndex((option) => option.label === candidate.label);

      if (twinIndex === -1) {
        options.push(candidate);
      } else if (candidate.value === this.preset()) {
        options[twinIndex] = candidate;
      }
    }

    if (this.optional()) {
      options.unshift({ label: this.transloco.translate('period.none'), value: 'none' });
    }

    return options;
  });

  readonly rangeFrom = computed(() => {
    const current = this.period();

    return current === null ? '' : formatSantiagoDay(current.from);
  });

  /** Last day the half-open range covers: a range ending at 00:00 ends the day before. */
  readonly rangeTo = computed(() => {
    const current = this.period();

    return current === null ? '' : formatSantiagoDay(new Date(new Date(current.to).getTime() - 1).toISOString());
  });

  ngOnInit(): void {
    const fromUrl = this.syncUrl() ? this.periodFromUrl() : null;

    if (fromUrl !== null) {
      this.preset.set('custom');
      this.period.set(fromUrl);
      this.customRange.set(this.daysOf(fromUrl));
    } else if (this.optional()) {
      this.preset.set('none');
    } else {
      this.preset.set('default');
      this.period.set(rollingPeriod(this.defaultDays(), new Date()));
    }

    if (this.emitInitial()) {
      this.periodChange.emit({ preset: this.preset(), period: this.period() });
    }
  }

  choose(preset: PeriodPreset): void {
    this.preset.set(preset);

    if (preset === 'custom') {
      const current = this.period();

      this.customRange.set(current === null ? null : this.daysOf(current));

      return;
    }

    this.customRange.set(null);
    this.apply(preset, this.rangeOf(preset));
  }

  onCustomRange(range: (Date | null)[] | null): void {
    this.customRange.set(range);

    const first = range?.[0] ?? null;

    const last = range?.[1] ?? null;

    if (first === null || last === null) {
      return;
    }

    const custom = customPeriod(first, last);

    if (custom === null) {
      return;
    }

    this.apply('custom', custom);
  }

  private rangeOf(preset: PeriodPreset): Period | null {
    const now = new Date();

    switch (preset) {
      case 'default':
        return rollingPeriod(this.defaultDays(), now);
      case 'thirty':
        return rollingPeriod(THIRTY_DAYS, now);
      case 'ninety':
        return rollingPeriod(NINETY_DAYS, now);
      default:
        return null;
    }
  }

  private apply(preset: PeriodPreset, period: Period | null): void {
    this.period.set(period);

    if (this.syncUrl()) {
      const queryParams = period === null ? { from: null, to: null } : { from: period.from, to: period.to };

      void this.router.navigate([], { relativeTo: this.route, queryParams, queryParamsHandling: 'merge', replaceUrl: true });
    }

    this.periodChange.emit({ preset, period });
  }

  private periodFromUrl(): Period | null {
    const query = this.route.snapshot.queryParamMap;

    return parsePeriod(query.get('from'), query.get('to'));
  }

  private daysOf(period: Period): Date[] {
    const first = santiagoDateOf(new Date(period.from));

    const last = santiagoDateOf(new Date(new Date(period.to).getTime() - 1));

    return [new Date(first.year, first.month - 1, first.day), new Date(last.year, last.month - 1, last.day)];
  }
}
