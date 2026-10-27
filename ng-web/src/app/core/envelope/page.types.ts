/** Data shape of every list response (`totalItems`, L12). */
export interface ApiPage<T> {
  items: T[];
  page: number;
  size: number;
  totalItems: number;
}
