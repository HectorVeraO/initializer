import { customPeriod, parsePeriod, rollingPeriod } from './period';

const DAY_MS = 24 * 60 * 60 * 1000;

describe('parsePeriod', () => {
  it('keeps a complete range as UTC instants', () => {
    expect(parsePeriod('2026-10-01T00:00:00Z', '2026-10-02T00:00:00Z')).toEqual({ from: '2026-10-01T00:00:00.000Z', to: '2026-10-02T00:00:00.000Z' });
  });

  it('rejects a malformed, missing or empty range', () => {
    expect(parsePeriod('yesterday', '2026-10-02T00:00:00Z')).toBeNull();
    expect(parsePeriod(null, '2026-10-02T00:00:00Z')).toBeNull();
    expect(parsePeriod('2026-10-02T00:00:00Z', '2026-10-01T00:00:00Z')).toBeNull();
    expect(parsePeriod('2026-10-02T00:00:00Z', '2026-10-02T00:00:00Z')).toBeNull();
  });
});

describe('rollingPeriod', () => {
  it('covers the last days up to the given instant', () => {
    const now = new Date('2026-10-04T03:00:00.000Z');

    const period = rollingPeriod(7, now);

    expect(period.to).toBe('2026-10-04T03:00:00.000Z');
    expect((new Date(period.to).getTime() - new Date(period.from).getTime()) / DAY_MS).toBe(7);
  });
});

describe('customPeriod', () => {
  it('turns the days picked in a calendar into Santiago days expressed in UTC', () => {
    const period = customPeriod(new Date(2026, 9, 1), new Date(2026, 9, 3));

    expect(period).toEqual({ from: '2026-10-01T03:00:00.000Z', to: '2026-10-04T03:00:00.000Z' });
  });

  it('includes the whole last day when first and last are the same day', () => {
    const period = customPeriod(new Date(2026, 9, 1), new Date(2026, 9, 1));

    expect(period).toEqual({ from: '2026-10-01T03:00:00.000Z', to: '2026-10-02T03:00:00.000Z' });
  });

  it('rejects a range that ends before it starts', () => {
    expect(customPeriod(new Date(2026, 9, 5), new Date(2026, 9, 1))).toBeNull();
  });
});
