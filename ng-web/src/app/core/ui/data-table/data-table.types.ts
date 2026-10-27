export type DataTableFilterType = 'text' | 'boolean' | 'date';

export interface DataTableColumn {
  field: string;
  labelKey: string;
  sortable?: boolean;
  filterType?: DataTableFilterType;
  width?: string;
}
