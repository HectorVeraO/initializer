import { MS_PER_DAY, santiagoMidnight } from '@/core/format/santiago-time';

/** A half-open range `[from, to)` of RFC 3339 UTC instants. */
export interface Period {
  from: string;
  to: string;
}

export type PeriodPreset = 'default' | 'thirty' | 'ninety' | 'custom' | 'none';

/** What the picker tells its host: the preset chosen and the range it stands for (null for "no range"). */
export interface PeriodSelection {
  preset: PeriodPreset;
  period: Period | null;
}

export const DEFAULT_PERIOD_DAYS = 7;

const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/** The range of the URL parameters, or null when either is missing, malformed or the range is empty. */
export function parsePeriod(from: string | null, to: string | null): Period | null {
  if (from === null || to === null || !RFC3339.test(from) || !RFC3339.test(to)) {
    return null;
  }

  const start = new Date(from);

  const end = new Date(to);

  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || start.getTime() >= end.getTime()) {
    return null;
  }

  return { from: start.toISOString(), to: end.toISOString() };
}

/** The `days` days that end at `now`. */
export function rollingPeriod(days: number, now: Date): Period {
  const end = new Date(now.getTime());

  const start = new Date(end.getTime() - days * MS_PER_DAY);

  return { from: start.toISOString(), to: end.toISOString() };
}

/** First and last day picked in a calendar (their local dates), as Santiago days: from 00:00 of the first to 00:00 after the last. */
export function customPeriod(firstDay: Date, lastDay: Date): Period | null {
  const start = santiagoMidnight(firstDay.getFullYear(), firstDay.getMonth() + 1, firstDay.getDate());

  const end = santiagoMidnight(lastDay.getFullYear(), lastDay.getMonth() + 1, lastDay.getDate() + 1);

  if (start.getTime() >= end.getTime()) {
    return null;
  }

  return { from: start.toISOString(), to: end.toISOString() };
}
