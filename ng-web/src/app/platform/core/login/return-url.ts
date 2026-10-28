/** Where the login page goes when `returnUrl` is missing or unsafe. */
export const DEFAULT_RETURN_URL = '/home';

// A backslash or a control character: browsers drop tabs and newlines inside a URL, which could turn "/\t/host" into "//host".
// eslint-disable-next-line no-control-regex
const UNSAFE_CHARACTERS = /[\\\u0000- \u007f]/;

/** `candidate` when it is a path inside this app: starts with one `/`, no `//`, no backslash, no control character. Anything else yields `/home`. */
export function safeReturnUrl(candidate: unknown): string {
  if (typeof candidate !== 'string') {
    return DEFAULT_RETURN_URL;
  }

  const isAppPath = candidate.startsWith('/') && !candidate.startsWith('//');

  if (!isAppPath || UNSAFE_CHARACTERS.test(candidate)) {
    return DEFAULT_RETURN_URL;
  }

  return candidate;
}
