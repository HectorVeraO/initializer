import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { configureFeatureTest, settle } from '@/core/testing/ui-testing';
import { PageRequest, SortRequest } from '@/core/api/api-params';
import { DataTableComponent } from './data-table.component';
import { RowToggleComponent } from './row-toggle.component';
import { DataTableColumn } from './data-table.types';

interface Row {
  key: string;
}

function rowsOf(count: number): Row[] {
  return Array.from({ length: count }, (_, index) => ({ key: `row-${String(index).padStart(2, '0')}` }));
}

/** Paginator link by its visible label, so the tests do not depend on the library's class names. */
function paginatorLink(root: HTMLElement, label: string): HTMLButtonElement | undefined {
  return Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find((button) => button.textContent?.trim() === label);
}

@Component({
  selector: 'p-data-table-host',
  standalone: true,
  imports: [DataTableComponent],
  template: `
    <p-data-table [columns]="columns" [value]="rows()" dataKey="key" [paginator]="paginator()" [showActionsColumn]="false">
      <ng-template #body let-row>
        <tr data-testid="row">
          <td>{{ row.key }}</td>
        </tr>
      </ng-template>
    </p-data-table>
  `
})
class HostComponent {
  readonly columns: DataTableColumn[] = [{ field: 'key', labelKey: 'common.back', sortable: true }];
  readonly rows = signal<Row[]>(rowsOf(12));
  readonly paginator = signal(true);
}

describe('DataTableComponent', () => {
  async function mount(setup: (host: HostComponent) => void = () => undefined): Promise<ComponentFixture<HostComponent>> {
    configureFeatureTest();

    const fixture = TestBed.createComponent(HostComponent);

    setup(fixture.componentInstance);
    await settle(fixture);

    return fixture;
  }

  async function render(setup: (host: HostComponent) => void = () => undefined): Promise<HTMLElement> {
    const fixture = await mount(setup);

    return fixture.nativeElement as HTMLElement;
  }

  function visibleKeys(root: HTMLElement): string[] {
    return Array.from(root.querySelectorAll('[data-testid="row"] td')).map((cell) => cell.textContent?.trim() ?? '');
  }

  it('pages the rows ten at a time by default', async () => {
    const root = await render();

    expect(root.querySelectorAll('[data-testid="row"]')).toHaveLength(10);
  });

  it('reports the visible range and the total', async () => {
    const root = await render();

    expect(root.textContent).toContain('Mostrando 1 a 10 de 12 registros');
  });

  it('shows every row when the paginator is turned off', async () => {
    const root = await render((host) => host.paginator.set(false));

    expect(root.querySelectorAll('[data-testid="row"]')).toHaveLength(12);
  });

  it('keeps the paginator footer when the rows fit one page', async () => {
    const root = await render((host) => host.rows.set(rowsOf(10)));

    expect(root.querySelectorAll('[data-testid="row"]')).toHaveLength(10);
    expect(root.textContent).toContain('Mostrando 1 a 10 de 10');
  });

  it('sorts across pages, not only the visible page', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    const header = root.querySelector<HTMLElement>('th');

    header?.click();
    await settle(fixture);
    header?.click();
    await settle(fixture);

    expect(visibleKeys(root)[0]).toBe('row-11');
  });

  it('goes back to the first page when a reload leaves fewer rows than the page offset', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    paginatorLink(root, 'Siguiente')?.click();
    await settle(fixture);

    expect(visibleKeys(root)[0]).toBe('row-10');

    fixture.componentInstance.rows.set(rowsOf(5));
    await settle(fixture);

    expect(visibleKeys(root)).toEqual(['row-00', 'row-01', 'row-02', 'row-03', 'row-04']);
  });
});

@Component({
  selector: 'p-lazy-data-table-host',
  standalone: true,
  imports: [DataTableComponent],
  template: `
    <p-data-table [columns]="columns" [value]="rows()" dataKey="key" [lazy]="true" [totalRecords]="total()" [first]="first()" [sort]="sort()" [showActionsColumn]="false" (pageRequest)="requests.push($event)">
      <ng-template #body let-row>
        <tr data-testid="row">
          <td>{{ row.key }}</td>
        </tr>
      </ng-template>
    </p-data-table>
  `
})
class LazyHostComponent {
  readonly columns: DataTableColumn[] = [{ field: 'key', labelKey: 'common.back', sortable: true }];
  readonly rows = signal<Row[]>(rowsOf(10));
  readonly total = signal(95);
  readonly first = signal(0);
  readonly sort = signal<SortRequest | null>(null);
  readonly requests: PageRequest[] = [];
}

describe('DataTableComponent lazy mode', () => {
  async function mount(): Promise<ComponentFixture<LazyHostComponent>> {
    configureFeatureTest();

    const fixture = TestBed.createComponent(LazyHostComponent);

    await settle(fixture);

    return fixture;
  }

  it('shows only the page it was given and the server total', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    expect(root.querySelectorAll('[data-testid="row"]')).toHaveLength(10);
    expect(root.textContent).toContain('Mostrando 1 a 10 de 95 registros');
  });

  it('does not ask for a page on its own', async () => {
    const fixture = await mount();

    expect(fixture.componentInstance.requests).toEqual([]);
  });

  it('asks for the next page with a one-based number', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    paginatorLink(root, 'Siguiente')?.click();
    await settle(fixture);

    expect(fixture.componentInstance.requests).toEqual([{ page: 2, pageSize: 10, sort: null }]);
  });

  it('asks for the sorted first page when a header is clicked', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    root.querySelector<HTMLElement>('th')?.click();
    await settle(fixture);

    expect(fixture.componentInstance.requests).toEqual([{ page: 1, pageSize: 10, sort: { field: 'key', direction: 'asc' } }]);
  });

  it('keeps the paginator footer when the total fits one page', async () => {
    const fixture = await mount();

    fixture.componentInstance.total.set(7);
    fixture.componentInstance.rows.set(rowsOf(7));
    await settle(fixture);

    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Mostrando 1 a 7 de 7');
  });
});

@Component({
  selector: 'p-data-table-expand-host',
  standalone: true,
  imports: [DataTableComponent, RowToggleComponent],
  template: `
    <p-data-table [columns]="columns" [value]="rows()" dataKey="key" [expandable]="expandable()" [showActionsColumn]="false" [paginator]="false" (rowExpand)="opened.push($event.key)">
      <ng-template #body let-row let-expanded="expanded" let-toggle="toggle">
        <tr [attr.data-testid]="'row-' + row.key">
          @if (expandable()) {
            <td><p-row-toggle [expanded]="expanded" [label]="'Mostrar ' + row.key" [testId]="'toggle-' + row.key" (toggled)="toggle()" /></td>
          }
          <td>{{ row.key }}</td>
        </tr>
      </ng-template>
      <ng-template #expandedRow let-row>
        <div [attr.data-testid]="'detail-' + row.key">detail of {{ row.key }}</div>
      </ng-template>
    </p-data-table>
  `
})
class ExpandHostComponent {
  readonly columns: DataTableColumn[] = [{ field: 'key', labelKey: 'common.back' }];
  readonly rows = signal<Row[]>(rowsOf(3));
  readonly expandable = signal(true);
  readonly opened: string[] = [];
}

describe('DataTableComponent row expansion', () => {
  async function mount(): Promise<ComponentFixture<ExpandHostComponent>> {
    configureFeatureTest();

    const fixture = TestBed.createComponent(ExpandHostComponent);

    await settle(fixture);

    return fixture;
  }

  function toggle(fixture: ComponentFixture<ExpandHostComponent>, key: string): HTMLButtonElement {
    return (fixture.nativeElement as HTMLElement).querySelector(`[data-testid="toggle-${key}"]`) as HTMLButtonElement;
  }

  function detail(fixture: ComponentFixture<ExpandHostComponent>, key: string): Element | null {
    return (fixture.nativeElement as HTMLElement).querySelector(`[data-testid="detail-${key}"]`);
  }

  it('keeps every row closed until its toggle is used and adds a column for the toggles', async () => {
    const fixture = await mount();

    const root = fixture.nativeElement as HTMLElement;

    expect(detail(fixture, 'row-00')).toBeNull();
    expect(toggle(fixture, 'row-00').getAttribute('aria-expanded')).toBe('false');
    expect(root.querySelectorAll('thead th')).toHaveLength(2);
  });

  it('opens a row into its detail template, tells the caller once, and closes it again', async () => {
    const fixture = await mount();

    toggle(fixture, 'row-01').click();
    await settle(fixture);

    expect(detail(fixture, 'row-01')?.textContent).toContain('detail of row-01');
    expect(detail(fixture, 'row-00')).toBeNull();
    expect(toggle(fixture, 'row-01').getAttribute('aria-expanded')).toBe('true');
    expect(fixture.componentInstance.opened).toEqual(['row-01']);

    toggle(fixture, 'row-01').click();
    await settle(fixture);

    expect(detail(fixture, 'row-01')).toBeNull();
    expect(fixture.componentInstance.opened).toEqual(['row-01']);
  });

  it('lets several rows stay open at once', async () => {
    const fixture = await mount();

    toggle(fixture, 'row-00').click();
    await settle(fixture);
    toggle(fixture, 'row-02').click();
    await settle(fixture);

    expect(detail(fixture, 'row-00')).not.toBeNull();
    expect(detail(fixture, 'row-02')).not.toBeNull();
  });

  it('keeps an open row open while a reload swaps the page for rows that still hold its key', async () => {
    const fixture = await mount();

    toggle(fixture, 'row-01').click();
    await settle(fixture);

    fixture.componentInstance.rows.set([...rowsOf(3)]);
    await settle(fixture);

    expect(detail(fixture, 'row-01')).not.toBeNull();
  });

  it('adds no toggle column when the table is not expandable', async () => {
    const fixture = await mount();

    fixture.componentInstance.expandable.set(false);
    await settle(fixture);

    expect((fixture.nativeElement as HTMLElement).querySelectorAll('thead th')).toHaveLength(1);
  });
});
