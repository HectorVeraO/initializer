import { santiagoDayStartFrom, santiagoMidnight, santiagoWeekStart } from './santiago-time';

describe('Santiago day boundaries', () => {
  it('starts a winter day at 04:00 UTC (UTC-4)', () => {
    expect(santiagoMidnight(2026, 8, 31).toISOString()).toBe('2026-08-31T04:00:00.000Z');
  });

  it('starts a summer day at 03:00 UTC (UTC-3)', () => {
    expect(santiagoMidnight(2026, 9, 7).toISOString()).toBe('2026-09-07T03:00:00.000Z');
  });

  it('starts the spring-change day at the first valid instant, 04:00 UTC, when midnight does not exist', () => {
    expect(santiagoMidnight(2026, 9, 6).toISOString()).toBe('2026-09-06T04:00:00.000Z');
  });

  it('starts an ordinary day at local midnight', () => {
    expect(santiagoMidnight(2026, 6, 15).toISOString()).toBe('2026-06-15T04:00:00.000Z');
  });

  it('makes the week that crosses the spring change last 167 hours', () => {
    const start = santiagoMidnight(2026, 8, 31);

    const end = santiagoMidnight(2026, 9, 7);

    expect((end.getTime() - start.getTime()) / 3_600_000).toBe(167);
  });

  it('overflows the day of the month like a calendar', () => {
    expect(santiagoMidnight(2026, 8, 32).toISOString()).toBe(santiagoMidnight(2026, 9, 1).toISOString());
  });

  it('goes back to the Monday 00:00 of seven weeks ago for an 8-week series', () => {
    const now = new Date('2026-10-24T15:00:00Z');

    const start = santiagoWeekStart(now, 7);

    expect(start.toISOString()).toBe('2026-08-31T04:00:00.000Z');
  });

  it('takes the Monday of the current week with zero weeks back', () => {
    const sunday = new Date('2026-10-25T20:00:00Z');

    expect(santiagoWeekStart(sunday, 0).toISOString()).toBe('2026-10-19T03:00:00.000Z');
  });

  it('counts days by the Santiago calendar', () => {
    const now = new Date('2026-10-24T02:00:00Z');

    expect(santiagoDayStartFrom(now, 0).toISOString()).toBe('2026-10-23T03:00:00.000Z');
  });
});
