import { OperatorFunction, map } from 'rxjs';
import type { ApiEnvelope } from './envelope.types';

/** A 2xx answer whose body is not an `ApiEnvelope` (a proxy page, an HTML error). */
export class UnexpectedResponseError extends Error {
  override readonly name = 'UnexpectedResponseError';
}

export function isApiEnvelope(body: unknown): body is ApiEnvelope {
  if (typeof body !== 'object' || body === null) {
    return false;
  }

  const record = body as Record<string, unknown>;

  const meta = record['meta'];

  return typeof meta === 'object' && meta !== null && 'traceId' in meta;
}

/** Maps an envelope to its `data`. A body that is not an envelope errors with `UnexpectedResponseError`; `data: null` passes as `null`. */
export function unwrapData<T>(): OperatorFunction<ApiEnvelope<T>, T> {
  return map((body: unknown): T => {
    if (!isApiEnvelope(body)) {
      throw new UnexpectedResponseError('The response is not an API envelope');
    }

    return body.data as T;
  });
}
