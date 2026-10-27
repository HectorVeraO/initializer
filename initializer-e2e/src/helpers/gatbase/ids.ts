import { randomBytes } from 'crypto';
import type { TestInfo } from '@playwright/test';

/**
 * Collision-free id of one test run: worker index and retry, then 8 random
 * lower-case hex characters. Parallel workers and retries never share one.
 */
export function uniqueId(testInfo: TestInfo): string {
  const id = `${testInfo.workerIndex}${testInfo.retry}${randomBytes(4).toString('hex')}`;
  return id;
}

/** Tenant slug of a run: `e2e-<id>`. */
export function tenantSlug(id: string): string {
  return `e2e-${id}`;
}

/** Synthetic address on the RFC 2606 example domain: `<name>.<id>@example.com`. */
export function emailFor(name: string, id: string): string {
  return `${name}.${id}@example.com`;
}

/** Synthetic password of a run: `E2e-pass-<id>`. */
export function passwordFor(id: string): string {
  return `E2e-pass-${id}`;
}
