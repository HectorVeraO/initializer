const SANTIAGO_TIME_ZONE = 'America/Santiago';

const partsFormatter = new Intl.DateTimeFormat('en-US', {
  timeZone: SANTIAGO_TIME_ZONE,
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: 'numeric',
  minute: 'numeric',
  second: 'numeric',
  hourCycle: 'h23'
});

const MS_PER_DAY = 24 * 60 * 60 * 1000;

/** Calendar date of a Santiago wall clock. `month` is 1-12. */
export interface LocalDate {
  year: number;
  month: number;
  day: number;
}

function localParts(instant: Date): Record<string, number> {
  const parts: Record<string, number> = {};

  for (const part of partsFormatter.formatToParts(instant)) {
    if (part.type !== 'literal') {
      parts[part.type] = Number(part.value);
    }
  }

  return parts;
}

/** Offset of America/Santiago from UTC at an instant, in milliseconds (negative while behind UTC). */
export function santiagoOffsetMs(instant: Date): number {
  const parts = localParts(instant);

  const asUtc = Date.UTC(parts['year'] ?? 0, (parts['month'] ?? 1) - 1, parts['day'] ?? 1, parts['hour'] ?? 0, parts['minute'] ?? 0, parts['second'] ?? 0);

  const whole = Math.floor(instant.getTime() / 1000) * 1000;

  return asUtc - whole;
}

/** The Santiago calendar date an instant falls on. */
export function santiagoDateOf(instant: Date): LocalDate {
  const parts = localParts(instant);

  return { year: parts['year'] ?? 0, month: parts['month'] ?? 1, day: parts['day'] ?? 1 };
}

/** The UTC instant at which a Santiago calendar day starts (`day` and `month` may overflow, as in `Date.UTC`). */
export function santiagoMidnight(year: number, month: number, day: number): Date {
  const wall = Date.UTC(year, month - 1, day, 0, 0, 0);

  const first = wall - santiagoOffsetMs(new Date(wall));

  const corrected = wall - santiagoOffsetMs(new Date(first));

  const isRealWallTime = santiagoOffsetMs(new Date(corrected)) === wall - corrected;

  if (isRealWallTime) {
    return new Date(corrected);
  }

  // Midnight falls in a DST gap: the day starts at the first valid instant, when the clock jumps forward.
  const afterGap = Math.max(first, corrected);

  return new Date(afterGap);
}

/** Start of the Santiago day that is `days` calendar days after the one `instant` falls on (negative goes back). */
export function santiagoDayStartFrom(instant: Date, days: number): Date {
  const local = santiagoDateOf(instant);

  return santiagoMidnight(local.year, local.month, local.day + days);
}

/** Monday 00:00 (Santiago) of the ISO week `weeksBack` weeks before the one `instant` falls in. */
export function santiagoWeekStart(instant: Date, weeksBack: number): Date {
  const local = santiagoDateOf(instant);

  const weekday = new Date(Date.UTC(local.year, local.month - 1, local.day)).getUTCDay();

  const sinceMonday = (weekday + 6) % 7;

  return santiagoMidnight(local.year, local.month, local.day - sinceMonday - weeksBack * 7);
}

export { MS_PER_DAY };
