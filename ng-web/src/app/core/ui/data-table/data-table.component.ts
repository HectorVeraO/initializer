import { Component, ContentChild, Input, TemplateRef, ViewChild, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TranslocoModule } from '@jsverse/transloco';
import { Table, TableModule } from '@openng/optimus-ui/table';
import { DataTableColumn } from './data-table.types';

/**
 * Thin wrapper over p-table that generates the sort/filter header row from a
 * declarative column list. Row content stays fully caller-defined via the
 * projected `#body` template, since cell content (tags, links, formatted
 * values, action buttons) varies too much across features to templatize.
 */
@Component({
  selector: 'p-data-table',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule, TranslocoModule, TableModule],
  templateUrl: './data-table.component.html'
})
export class DataTableComponent<T> {
  @Input() columns: DataTableColumn[] = [];
  @Input() value: T[] = [];
  @Input() loading = false;
  @Input() dataKey = '_id';
  @Input() rows = 10;
  @Input() rowsPerPageOptions = [10, 25, 50];
  @Input() emptyMessageKey = '';
  @Input() showActionsColumn = true;
  @Input() actionsColumnWidth = '10rem';
  @Input() styleClass = 'p-datatable-gridlines';
  @Input() globalFilterFields: string[] = [];

  @ContentChild('body') bodyTemplate?: TemplateRef<{ $implicit: T }>;
  @ViewChild('innerTable') private innerTable!: Table;

  get colspan(): number {
    return this.columns.length + (this.showActionsColumn ? 1 : 0);
  }

  /** Clears active sort/filter state on the underlying p-table. */
  clear(): void {
    this.innerTable.clear();
  }

  /** Resets pagination to the first page without touching sort/filter state. */
  resetPage(): void {
    this.innerTable.first = 0;
  }
}
