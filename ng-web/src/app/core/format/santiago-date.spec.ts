import { formatSantiago } from './santiago-date';

describe('formatSantiago', () => {
  it('shows a UTC instant in Chilean summer time (UTC-3)', () => {
    expect(formatSantiago('2026-01-15T15:00:00Z')).toBe('15/01/2026 12:00:00');
  });

  it('shows a UTC instant in Chilean winter time (UTC-4)', () => {
    expect(formatSantiago('2026-07-15T15:00:00Z')).toBe('15/07/2026 11:00:00');
  });

  it('crosses the date boundary backwards', () => {
    expect(formatSantiago('2026-07-15T02:30:00Z')).toBe('14/07/2026 22:30:00');
  });

  it('writes local midnight as 00 and never as 24', () => {
    expect(formatSantiago('2026-07-15T04:00:00Z')).toBe('15/07/2026 00:00:00');
  });

  it('returns unparseable input unchanged', () => {
    expect(formatSantiago('not a date')).toBe('not a date');
  });
});
