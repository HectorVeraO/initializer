import { HttpParams } from '@angular/common/http';

export type ParamValue = string | number | null | undefined;

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
