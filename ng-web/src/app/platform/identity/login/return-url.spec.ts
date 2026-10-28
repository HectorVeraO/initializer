import { DEFAULT_RETURN_URL, safeReturnUrl } from './return-url';

describe('safeReturnUrl', () => {
  it.each(['/memberships', '/home', '/operator/tenants?page=2', '/a/b#frag'])('keeps the app path %s', (path) => {
    expect(safeReturnUrl(path)).toBe(path);
  });

  it.each([
    null,
    undefined,
    '',
    'home',
    '//evil.example.com',
    '/\\evil',
    'https://evil.example.com',
    'javascript:alert(1)',
    '/\t/evil.example.com',
    '/\n/evil.example.com',
    '/\r/evil.example.com',
    '/ /evil.example.com',
    '/a\\b',
    '/\u0000',
    '/\u007f',
    42
  ])('falls back to /home for %j', (candidate) => {
    expect(safeReturnUrl(candidate)).toBe(DEFAULT_RETURN_URL);
  });
});
