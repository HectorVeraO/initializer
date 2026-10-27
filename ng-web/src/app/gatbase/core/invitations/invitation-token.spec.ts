import { tokenFromHash } from './invitation-token';

/** Small deterministic generator so the property runs are repeatable. */
function sequence(seed: number): () => number {
  let state = seed;

  return () => {
    state = (state * 1664525 + 1013904223) % 4294967296;

    return state / 4294967296;
  };
}

const PIECES = ['', '#', 'token', '=', '&', 'a', 'b=c', '%20', '%3D', '%26', '+', 'token=', 'x', '?token=', ';', 'é'];

describe('tokenFromHash', () => {
  it.each([
    ['#token=synthetic-token-value', 'synthetic-token-value'],
    ['token=synthetic-token-value', 'synthetic-token-value'],
    ['#a=1&token=synthetic-token-value&b=2', 'synthetic-token-value'],
    ['#token=a%2Fb%3D', 'a/b=']
  ])('reads the token of %s', (hash, expected) => {
    expect(tokenFromHash(hash)).toBe(expected);
  });

  it.each(['', '#', '#token=', '#token', '#other=1', '#x=token=1'])('returns null for %j', (hash) => {
    expect(tokenFromHash(hash)).toBeNull();
  });

  it('returns exactly the encoded token among generated fragments', () => {
    const next = sequence(7);

    const values = ['a', 'a/b=', 'x y', '�', 'a&b', '%41', 'tok+en'];

    for (let run = 0; run < 500; run += 1) {
      const value = values[Math.floor(next() * values.length)];

      const before = next() < 0.5 ? 'other=1&' : '';

      const after = next() < 0.5 ? '&b=2' : '';

      expect(tokenFromHash(`#${before}token=${encodeURIComponent(value)}${after}`)).toBe(value);
    }
  });

  it('returns the first token parameter, or null, for every generated fragment', () => {
    const next = sequence(11);

    for (let run = 0; run < 500; run += 1) {
      const length = Math.floor(next() * 6);

      const hash = Array.from({ length }, () => PIECES[Math.floor(next() * PIECES.length)]).join('');

      // URLSearchParams drops one leading '?' of its input.
      const fragment = (hash.startsWith('#') ? hash.slice(1) : hash).replace(/^\?/, '');

      const firstToken = fragment.split('&').find((part) => part.startsWith('token='));

      const rawValue = firstToken === undefined ? '' : firstToken.slice('token='.length);

      const expected = rawValue === '' ? null : decodeURIComponent(rawValue.replace(/\+/g, ' '));

      expect(tokenFromHash(hash)).toBe(expected);
    }
  });
});
