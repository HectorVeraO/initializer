/** Path the accept page strips the token down to. */
export const ACCEPT_PATH = '/invitations/accept';

/** The `token` parameter of a URL fragment (`#token=...`), or null when it is absent or empty. The query string is never read. */
export function tokenFromHash(hash: string): string | null {
  const fragment = hash.startsWith('#') ? hash.slice(1) : hash;

  const token = new URLSearchParams(fragment).get('token');

  return token === null || token === '' ? null : token;
}
