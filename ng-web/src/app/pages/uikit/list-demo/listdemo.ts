import { CommonModule } from '@angular/common';
import { Component, inject, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DataViewModule } from '@openng/optimus-ui/dataview';
import { OrderListModule } from '@openng/optimus-ui/orderlist';
import { PickListModule } from '@openng/optimus-ui/picklist';
import { SelectButtonModule } from '@openng/optimus-ui/selectbutton';
import { TagModule } from '@openng/optimus-ui/tag';
import { Product, ProductService } from '../../service/product.service';

@Component({
  selector: 'p-list-demo',
  standalone: true,
  imports: [CommonModule, DataViewModule, FormsModule, SelectButtonModule, PickListModule, OrderListModule, TagModule, ButtonModule],
  templateUrl: './listdemo.html',
  styles: `
    ::ng-deep {
      .p-orderlist-list-container {
        width: 100%;
      }
    }
  `,
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [ProductService]
})
export class ListDemo implements OnInit {
  layout: 'list' | 'grid' = 'list';

  options = ['list', 'grid'];

  readonly products = signal<Product[]>([]);

  sourceCities: { name: string; code: string }[] = [];

  targetCities: { name: string; code: string }[] = [];

  orderCities: { name: string; code: string }[] = [];

  private productService = inject(ProductService);

  ngOnInit() {
    this.productService.getProductsSmall().then((data) => this.products.set(data.slice(0, 6)));

    this.sourceCities = [
      { name: 'San Francisco', code: 'SF' },
      { name: 'London', code: 'LDN' },
      { name: 'Paris', code: 'PRS' },
      { name: 'Istanbul', code: 'IST' },
      { name: 'Berlin', code: 'BRL' },
      { name: 'Barcelona', code: 'BRC' },
      { name: 'Rome', code: 'RM' }
    ];

    this.targetCities = [];

    this.orderCities = [
      { name: 'San Francisco', code: 'SF' },
      { name: 'London', code: 'LDN' },
      { name: 'Paris', code: 'PRS' },
      { name: 'Istanbul', code: 'IST' },
      { name: 'Berlin', code: 'BRL' },
      { name: 'Barcelona', code: 'BRC' },
      { name: 'Rome', code: 'RM' }
    ];
  }

  getSeverity(product: Product) {
    switch (product.inventoryStatus) {
      case 'INSTOCK':
        return 'success';

      case 'LOWSTOCK':
        return 'warn';

      case 'OUTOFSTOCK':
        return 'danger';

      default:
        return 'info';
    }
  }
}
