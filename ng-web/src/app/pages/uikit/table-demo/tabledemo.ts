import { Component, ElementRef, inject, OnInit, ViewChild, ChangeDetectionStrategy, signal } from '@angular/core';
import { ConfirmationService, MessageService } from '@openng/optimus-ui/api';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MultiSelectModule } from '@openng/optimus-ui/multiselect';
import { SelectModule } from '@openng/optimus-ui/select';
import { SliderModule } from '@openng/optimus-ui/slider';
import { Table, TableModule } from '@openng/optimus-ui/table';
import { ProgressBarModule } from '@openng/optimus-ui/progressbar';
import { ToggleButtonModule } from '@openng/optimus-ui/togglebutton';
import { ToastModule } from '@openng/optimus-ui/toast';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ButtonModule } from '@openng/optimus-ui/button';
import { RatingModule } from '@openng/optimus-ui/rating';
import { RippleModule } from '@openng/optimus-ui/ripple';
import { InputIconModule } from '@openng/optimus-ui/inputicon';
import { IconFieldModule } from '@openng/optimus-ui/iconfield';
import { TagModule } from '@openng/optimus-ui/tag';
import { Customer, CustomerService, Representative } from '../../service/customer.service';
import { Product, ProductService } from '../../service/product.service';
import { ObjectUtils } from '@openng/optimus-ui/utils';

type expandedRows = Record<string, boolean>;

@Component({
  selector: 'p-table-demo',
  standalone: true,
  imports: [
    TableModule,
    MultiSelectModule,
    SelectModule,
    InputIconModule,
    TagModule,
    InputTextModule,
    SliderModule,
    ProgressBarModule,
    ToggleButtonModule,
    ToastModule,
    CommonModule,
    FormsModule,
    ButtonModule,
    RatingModule,
    RippleModule,
    IconFieldModule
  ],
  templateUrl: './tabledemo.html',
  styles: `
    .p-datatable-frozen-tbody {
      font-weight: bold;
    }

    .p-datatable-scrollable .p-frozen-column {
      font-weight: bold;
    }
  `,
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [ConfirmationService, MessageService, CustomerService, ProductService]
})
export class TableDemo implements OnInit {
  readonly customers1 = signal<Customer[]>([]);

  readonly customers2 = signal<Customer[]>([]);

  readonly customers3 = signal<Customer[]>([]);

  selectedCustomers1: Customer[] = [];

  selectedCustomer: Customer = {};

  representatives: Representative[] = [];

  statuses: { label: string; value: string }[] = [];

  readonly products = signal<Product[]>([]);

  rowGroupMetadata: Record<string, { index: number; size: number }> = {};

  expandedRows: expandedRows = {};

  activityValues: number[] = [0, 100];

  isExpanded = false;

  balanceFrozen = false;

  readonly loading = signal(true);

  @ViewChild('filter') filter!: ElementRef;

  private customerService = inject(CustomerService);

  private productService = inject(ProductService);

  ngOnInit() {
    this.customerService.getCustomersLarge().then((customers) => {
      // @ts-expect-error: date field not in generated type but present at runtime
      customers.forEach((customer) => (customer.date = new Date(customer.date)));
      this.customers1.set(customers);
      this.loading.set(false);
    });
    this.customerService.getCustomersMedium().then((customers) => this.customers2.set(customers));
    this.customerService.getCustomersLarge().then((customers) => this.customers3.set(customers));
    this.productService.getProductsWithOrdersSmall().then((data) => this.products.set(data));

    this.representatives = [
      { name: 'Amy Elsner', image: 'amyelsner.png' },
      { name: 'Anna Fali', image: 'annafali.png' },
      { name: 'Asiya Javayant', image: 'asiyajavayant.png' },
      { name: 'Bernardo Dominic', image: 'bernardodominic.png' },
      { name: 'Elwin Sharvill', image: 'elwinsharvill.png' },
      { name: 'Ioni Bowcher', image: 'ionibowcher.png' },
      { name: 'Ivan Magalhaes', image: 'ivanmagalhaes.png' },
      { name: 'Onyama Limba', image: 'onyamalimba.png' },
      { name: 'Stephen Shaw', image: 'stephenshaw.png' },
      { name: 'XuXue Feng', image: 'xuxuefeng.png' }
    ];

    this.statuses = [
      { label: 'Unqualified', value: 'unqualified' },
      { label: 'Qualified', value: 'qualified' },
      { label: 'New', value: 'new' },
      { label: 'Negotiation', value: 'negotiation' },
      { label: 'Renewal', value: 'renewal' },
      { label: 'Proposal', value: 'proposal' }
    ];
  }

  onSort() {
    this.updateRowGroupMetaData();
  }

  updateRowGroupMetaData() {
    this.rowGroupMetadata = {};

    const customers = this.customers3();

    if (customers) {
      for (let i = 0; i < customers.length; i++) {
        const rowData = customers[i];

        const representativeName = rowData?.representative?.name || '';

        if (i === 0) {
          this.rowGroupMetadata[representativeName] = { index: 0, size: 1 };
        } else {
          const previousRowData = customers[i - 1];

          const previousRowGroup = previousRowData?.representative?.name;

          if (representativeName === previousRowGroup) {
            this.rowGroupMetadata[representativeName].size++;
          } else {
            this.rowGroupMetadata[representativeName] = { index: i, size: 1 };
          }
        }
      }
    }
  }

  expandAll() {
    if (ObjectUtils.isEmpty(this.expandedRows)) {
      this.expandedRows = this.products().reduce(
        (acc, p) => {
          if (p.id) {
            acc[p.id] = true;
          }

          return acc;
        },
        {} as Record<string, boolean>
      );
      this.isExpanded = true;
    } else {
      this.collapseAll();
    }
  }

  collapseAll() {
    this.expandedRows = {};
    this.isExpanded = false;
  }

  formatCurrency(value: number) {
    return value.toLocaleString('en-US', { style: 'currency', currency: 'USD' });
  }

  onGlobalFilter(table: Table, event: Event) {
    table.filterGlobal((event.target as HTMLInputElement).value, 'contains');
  }

  clear(table: Table) {
    table.clear();
    this.filter.nativeElement.value = '';
  }

  getSeverity(status: string) {
    switch (status) {
      case 'qualified':
      case 'instock':
      case 'INSTOCK':
      case 'DELIVERED':
      case 'delivered':
        return 'success';

      case 'negotiation':
      case 'lowstock':
      case 'LOWSTOCK':
      case 'PENDING':
      case 'pending':
        return 'warn';

      case 'unqualified':
      case 'outofstock':
      case 'OUTOFSTOCK':
      case 'CANCELLED':
      case 'cancelled':
        return 'danger';

      default:
        return 'info';
    }
  }

  calculateCustomerTotal(name: string) {
    let total = 0;

    const customers = this.customers2();

    if (customers) {
      for (const customer of customers) {
        if (customer.representative?.name === name) {
          total++;
        }
      }
    }

    return total;
  }
}
