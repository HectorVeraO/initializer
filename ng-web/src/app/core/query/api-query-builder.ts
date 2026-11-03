import { SortDirection } from '../api/api-params';

/**
 * The serialized output of {@link ApiQueryBuilder}.
 *
 * Keys are the query-parameter names of the API (`page`, `size`, `sort`, plus
 * whatever a caller adds with `param`), values are primitives consumable by
 * Angular `HttpClient` through `buildParams`, by native `fetch` or by Axios.
 */
export type ApiQueryParams = Record<string, string | number>;

interface SortEntry {
  field: string;
  direction: SortDirection;
}

/**
 * Fluent builder for the query parameters of a list request.
 *
 * Framework-agnostic: outputs a plain `Record<string, string | number>`.
 * Ephemeral: instantiate per request to prevent query contamination.
 *
 * It emits only what a list contract of this API accepts: `page`, `size` and
 * `sort` are the shared paging parameters, and anything a single endpoint adds
 * (a status filter, a tenant id) goes through `param`. The conventions are the
 * ones of this API: `page` starts at 1 and `sort` is the field name, prefixed
 * with `-` when the order is descending.
 *
 * @example
 * ```ts
 * const params = new ApiQueryBuilder()
 *   .setPage(1)
 *   .setSize(20)
 *   .sort('createdAt', 'desc')
 *   .param('status', 'active')
 *   .build();
 * // { page: 1, size: 20, sort: '-createdAt', status: 'active' }
 * ```
 */
export class ApiQueryBuilder {
  private pageNumber: number | undefined;
  private pageSize: number | undefined;
  private sortEntry: SortEntry | undefined;
  private extras: Record<string, string | number> = {};

  /** Sets the page number; the first page is 1. */
  setPage(value: number): this {
    this.pageNumber = value;

    return this;
  }

  /** Sets the page size (number of items per page). */
  setSize(value: number): this {
    this.pageSize = value;

    return this;
  }

  /**
   * Sets the sort clause. A list is sorted by one field: a later call replaces
   * the previous one.
   *
   * @param field - Entity field to sort by.
   * @param direction - `'asc'` (default) or `'desc'`.
   */
  sort(field: string, direction: SortDirection = 'asc'): this {
    this.sortEntry = { field, direction };

    return this;
  }

  /** Removes the sort clause. */
  clearSort(): this {
    this.sortEntry = undefined;

    return this;
  }

  /**
   * Adds a parameter of the endpoint being called (a filter, a tenant id, an
   * include list): the builder never invents one the contract does not declare.
   */
  param(key: string, value: string | number): this {
    this.extras[key] = value;

    return this;
  }

  /**
   * Serializes all accumulated parameters into a flat dictionary.
   *
   * Only the parameters that were set are included; a descending sort is sent
   * as `-field`.
   */
  build(): ApiQueryParams {
    const params: ApiQueryParams = {};

    if (this.pageNumber !== undefined) {
      params['page'] = this.pageNumber;
    }

    if (this.pageSize !== undefined) {
      params['size'] = this.pageSize;
    }

    if (this.sortEntry !== undefined) {
      params['sort'] = this.sortEntry.direction === 'desc' ? `-${this.sortEntry.field}` : this.sortEntry.field;
    }

    /*
     * Extras merge last. Collisions with built-in keys are intentional:
     * the caller explicitly chose to override via .param().
     */
    for (const [key, value] of Object.entries(this.extras)) {
      params[key] = value;
    }

    return params;
  }
}
