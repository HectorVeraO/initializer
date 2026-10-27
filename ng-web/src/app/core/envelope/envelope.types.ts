/** One problem of an invalid request: the JSON path of the field and the failed rule. */
export interface ApiIssue {
  field: string;
  code: string;
  message: string;
}

/** Failure part of the envelope; a `422` lists every problem in `issues`. */
export interface ApiErrorBody {
  code: string;
  message: string;
  issues: ApiIssue[];
}

export interface Meta {
  traceId: string;
  timestamp: string;
}

/** Every `/api/v1/` response: `data` on success, `error` on failure, never both. */
export interface ApiEnvelope<T = unknown> {
  data: T | null;
  meta: Meta;
  error: ApiErrorBody | null;
}
