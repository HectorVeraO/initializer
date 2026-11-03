import { HttpParams } from '@angular/common/http';

export type ParamValue = string | number | null | undefined;

/** Order of a list request; a descending sort is sent as `-field`. */
export type SortDirection = 'asc' | 'desc';

export interface SortRequest {
  field: string;
  direction: SortDirection;
}

/** The page a list asks the server for: `page` starts at 1 and `sort` is null when the server decides. */
export interface PageRequest {
  page: number;
  pageSize: number;
  sort: SortRequest | null;
}

/** Query parameters of a request: empty, null and undefined values are left out. */
export function buildParams(values: Record<string, ParamValue>): HttpParams {
  let params = new HttpParams();

  for (const [name, value] of Object.entries(values)) {
    if (value === null || value === undefined) {
      continue;
    }

    const text = String(value).trim();

    if (text.length > 0) {
      params = params.set(name, text);
    }
  }

  return params;
}
