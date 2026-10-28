import en from './en.json';
import es from './es.json';

function keysOf(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return [prefix];
  }

  const keys = Object.entries(value as Record<string, unknown>).flatMap(([name, child]) => keysOf(child, prefix === '' ? name : `${prefix}.${name}`));

  return keys;
}

describe('platform core api-keys translations', () => {
  it('have the same keys in Spanish and English', () => {
    const spanish = keysOf(es).sort();

    const english = keysOf(en).sort();

    expect(english).toEqual(spanish);
  });
});
