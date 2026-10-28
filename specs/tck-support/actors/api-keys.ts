import { randomBytes } from 'node:crypto';
import type { APIRequestContext } from '@playwright/test';
import { baseUrl } from './index';
import type { Playwright } from './index';

const BASE32_ALPHABET = 'abcdefghijklmnopqrstuvwxyz234567';

function randomBase32(length: number): string {
  return Array.from(randomBytes(length), (byte) => BASE32_ALPHABET[byte % BASE32_ALPHABET.length]).join('');
}

/** Standalone context sending only X-API-Key (no cookies). */
export async function apiKeyClient(playwright: Playwright, plainTextKey: string): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL: baseUrl(), extraHTTPHeaders: { 'X-API-Key': plainTextKey } });
}

/** A random key in the api-keys format (^ak_live_[a-z2-7]{16}_[a-z2-7]{52}$) that no instance issued. */
export function wellFormedFakeKey(): string {
  return `ak_live_${randomBase32(16)}_${randomBase32(52)}`;
}
