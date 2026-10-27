import { displayValue, jsonText, parseJsonText } from './json-value';

/** Small deterministic generator (mulberry32) so a failing case replays. */
function randomSource(seed: number): () => number {
  let state = seed;

  return () => {
    state = (state + 0x6d2b79f5) | 0;

    let mixed = Math.imul(state ^ (state >>> 15), 1 | state);

    mixed = (mixed + Math.imul(mixed ^ (mixed >>> 7), 61 | mixed)) ^ mixed;

    const unit = ((mixed ^ (mixed >>> 14)) >>> 0) / 4294967296;

    return unit;
  };
}

const TEXT_POOL = ['', 'a', 'Jane Doe', 'quote " and \\ slash', 'line\nbreak', 'tab\t', 'éñ', '\u{1F600}', '</script>', '{"nested":1}'];

function randomJson(next: () => number, depth: number): unknown {
  const choice = Math.floor(next() * (depth > 0 ? 7 : 5));

  switch (choice) {
    case 0:
      return null;
    case 1:
      return next() < 0.5;
    case 2:
      return Math.floor(next() * 2000000) - 1000000;
    case 3:
      return (next() - 0.5) * 1e6;
    case 4:
      return TEXT_POOL[Math.floor(next() * TEXT_POOL.length)];
    case 5:
      return Array.from({ length: Math.floor(next() * 4) }, () => randomJson(next, depth - 1));
    default:
      return Object.fromEntries(Array.from({ length: Math.floor(next() * 4) }, (_, index) => [`k${index}${TEXT_POOL[Math.floor(next() * TEXT_POOL.length)]}`, randomJson(next, depth - 1)]));
  }
}

describe('json editor text', () => {
  it('reads back any generated JSON value as an equal value', () => {
    const next = randomSource(20260101);

    for (let round = 0; round < 500; round += 1) {
      const value = randomJson(next, 3);

      const parsed = parseJsonText(jsonText(value));

      expect(parsed).toEqual({ ok: true, value });
    }
  });

  it('is not ok for empty, blank and malformed text', () => {
    for (const text of ['', '   ', '{', '[1,', "{'a':1}", 'undefined', '1 2']) {
      expect(parseJsonText(text)).toEqual({ ok: false });
    }
  });

  it('accepts surrounding whitespace and the JSON literals', () => {
    expect(parseJsonText(' 7 ')).toEqual({ ok: true, value: 7 });
    expect(parseJsonText('null')).toEqual({ ok: true, value: null });
    expect(parseJsonText('"x"')).toEqual({ ok: true, value: 'x' });
  });

  it('shows strings as they are and everything else as compact JSON', () => {
    expect(displayValue('abc')).toBe('abc');
    expect(displayValue(5)).toBe('5');
    expect(displayValue(false)).toBe('false');
    expect(displayValue({ a: [1, 2] })).toBe('{"a":[1,2]}');
    expect(displayValue(null)).toBe('null');
  });
});
