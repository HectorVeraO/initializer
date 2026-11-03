import { Component, ContentChild, EventEmitter, Input, OnChanges, Output, SimpleChanges, TemplateRef, ViewChild, ChangeDetectionStrategy, ChangeDetectorRef, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TranslocoModule } from '@jsverse/transloco';
import { Table, TableLazyLoadEvent, TableModule } from '@openng/optimus-ui/table';
import { PageRequest, SortRequest } from '@/core/api/api-params';
import { DataTableColumn } from './data-table.types';

/** What the projected `#body` template receives for each row. */
export interface RowContext<T> {
  $implicit: T;
  /** True while the row is open (only with `expandable`). */
  expanded: boolean;
  /** Opens or closes the row (only with `expandable`). */
  toggle: () => void;
}

/**
 * Thin wrapper over p-table that generates the sort/filter header row from a
 * declarative column list. Row content stays fully caller-defined via the
 * projected `#body` template, since cell content (tags, links, formatted
 * values, action buttons) varies too much across features to templatize.
 *
 * Two modes. Default: the caller hands over every row and the table pages,
 * sorts and filters them in the browser. Lazy (`lazy`): the caller hands over
 * one page, `totalRecords` is the server's total and the table only asks for
 * another page (`pageRequest`) when the page, the page size or the sort
 * changes; it never loads on its own, the caller issues the first request.
 * Column filters are a browser feature and are not rendered in lazy mode.
 *
 * Optional row expansion (`expandable`): the projected `#body` template gets
 * `expanded` and `toggle` in its context (pair them with `p-row-toggle` in the
 * first cell of the row), and the projected `#expandedRow` template draws the
 * detail under an open row. Open rows are kept by `dataKey`, so they stay open
 * while a reload swaps the page; `rowExpand` tells the caller a row opened so
 * it can load the detail then, not before.
 */
@Component({
  selector: 'p-data-table',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule, TranslocoModule, TableModule],
  templateUrl: './data-table.component.html'
})
export class DataTableComponent<T> implements OnChanges {
  @Input() columns: DataTableColumn[] = [];
  @Input() value: T[] = [];
  @Input() loading = false;
  @Input() paginator = true;
  @Input() dataKey = '_id';
  @Input() rows = 10;
  @Input() rowsPerPageOptions = [10, 25, 50];
  @Input() emptyMessageKey = '';
  @Input() showActionsColumn = true;
  @Input() actionsColumnWidth = '10rem';
  @Input() styleClass = '';
  /** Inline style of the inner `<table>` (p-table `tableStyle`), e.g. a fixed layout so sibling tables align. */
  @Input() tableStyle: Record<string, string> | null = null;
  @Input() globalFilterFields: string[] = [];
  /** Server-side paging: `value` is the current page only and `pageRequest` asks the caller for the next one. */
  @Input() lazy = false;
  /** Rows matching the filters across every page (lazy mode). */
  @Input() totalRecords = 0;
  /** Zero-based offset of the first row of the current page (lazy mode), so the caller can send the table back to page one. */
  @Input() first = 0;
  /** Active server sort, shown in the header (lazy mode). */
  @Input() sort: SortRequest | null = null;

  /** Adds the toggle column and lets a row open into the `#expandedRow` template. */
  @Input() expandable = false;

  /** Page, page size or sort changed in lazy mode; the caller loads that page. */
  @Output() readonly pageRequest = new EventEmitter<PageRequest>();

  /** A row was opened (not closed); the caller loads its detail. */
  @Output() readonly rowExpand = new EventEmitter<T>();

  @ContentChild('body') bodyTemplate?: TemplateRef<RowContext<T>>;
  @ContentChild('expandedRow') expandedTemplate?: TemplateRef<{ $implicit: T }>;
  @ViewChild('innerTable') private innerTable!: Table;

  private readonly changes = inject(ChangeDetectorRef);

  /** Keys (the `dataKey` value) of the open rows; replaced, never mutated, so the table sees each change. */
  expandedKeys: Record<string, boolean> = {};

  /** A reload that leaves fewer rows than the current page offset (a filter, a delete) goes back to the first page. */
  ngOnChanges(changes: SimpleChanges): void {
    const valueChanged = !this.lazy && changes['value'] !== undefined && this.innerTable !== undefined;

    const isPastLastRow = valueChanged && (this.innerTable.first ?? 0) >= this.value.length;

    if (isPastLastRow) {
      this.innerTable.first = 0;
    }
  }

  /**
   * The paginator footer ("Mostrando X a Y de Z registros", rows per page, pager)
   * is always there, also when every row fits one page, as in the design system's
   * table. Its left/right layout comes from the theme (`app-theme.preset.ts`).
   */
  get showPaginator(): boolean {
    return this.paginator;
  }

  get sortField(): string | undefined {
    const field = this.sort?.field;

    return field;
  }

  get sortOrder(): number {
    const order = this.sort?.direction === 'desc' ? -1 : 1;

    return order;
  }

  /** Translates the p-table lazy event (offset, rows, sort) into the 1-based page request of the API. */
  onLazyLoad(event: TableLazyLoadEvent): void {
    const pageSize = event.rows ?? this.rows;

    const offset = event.first ?? 0;

    const sortField = typeof event.sortField === 'string' ? event.sortField : null;

    const sort: SortRequest | null = sortField ? { field: sortField, direction: event.sortOrder === -1 ? 'desc' : 'asc' } : null;

    this.pageRequest.emit({ page: Math.floor(offset / pageSize) + 1, pageSize, sort });
  }

  get colspan(): number {
    return this.columns.length + (this.showActionsColumn ? 1 : 0) + (this.expandable ? 1 : 0);
  }

  /** Context of one body row: the row plus its expansion state and the switch of it. */
  rowContext(row: T): RowContext<T> {
    return { $implicit: row, expanded: this.isExpanded(row), toggle: () => this.toggleRow(row) };
  }

  isExpanded(row: T): boolean {
    return this.expandedKeys[this.keyOf(row)] === true;
  }

  /** Opens a closed row (and tells the caller) or closes an open one. */
  toggleRow(row: T): void {
    const key = this.keyOf(row);

    const next = { ...this.expandedKeys };

    if (next[key] === true) {
      delete next[key];
      this.expandedKeys = next;
      this.changes.markForCheck();

      return;
    }

    next[key] = true;
    this.expandedKeys = next;
    this.changes.markForCheck();
    this.rowExpand.emit(row);
  }

  private keyOf(row: T): string {
    const value = (row as Record<string, unknown>)[this.dataKey];

    return String(value);
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
