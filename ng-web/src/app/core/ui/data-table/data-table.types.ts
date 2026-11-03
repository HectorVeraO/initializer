export type DataTableFilterType = 'text' | 'boolean' | 'date';

/**
 * `tableStyle` of a table whose rows never wrap: fixed layout without a minimum
 * width, so a cell with `truncate` ends in an ellipsis instead of widening its
 * column. Every column but one declares `width`; that one takes the rest.
 */
export const FIXED_TABLE_STYLE: Record<string, string> = { 'table-layout': 'fixed', width: '100%' };

export interface DataTableColumn {
  field: string;
  labelKey: string;
  sortable?: boolean;
  filterType?: DataTableFilterType;
  width?: string;
  /** Aligns the header label to the right, for columns whose cells are right-aligned numbers. */
  align?: 'right';
}
