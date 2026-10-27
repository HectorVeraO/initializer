import { Component, inject, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { ConfirmationService, MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { ToastModule } from '@openng/optimus-ui/toast';
import { DrawerModule } from '@openng/optimus-ui/drawer';
import { Popover, PopoverModule } from '@openng/optimus-ui/popover';
import { ConfirmPopupModule } from '@openng/optimus-ui/confirmpopup';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { FormsModule } from '@angular/forms';
import { TooltipModule } from '@openng/optimus-ui/tooltip';
import { TableModule, TableRowSelectEvent } from '@openng/optimus-ui/table';
import { Product, ProductService } from '../../service/product.service';

interface GalleryImage {
  source: string;
  thumbnail: string;
  title: string;
}

@Component({
  selector: 'p-overlay-demo',
  standalone: true,
  imports: [ToastModule, DialogModule, ButtonModule, DrawerModule, PopoverModule, ConfirmPopupModule, InputTextModule, FormsModule, TooltipModule, TableModule, ToastModule],
  templateUrl: './overlaydemo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [ConfirmationService, MessageService, ProductService]
})
export class OverlayDemo implements OnInit {
  images: GalleryImage[] = [];

  display = false;

  readonly products = signal<Product[]>([]);

  visibleLeft = false;

  visibleRight = false;

  visibleTop = false;

  visibleBottom = false;

  visibleFull = false;

  displayConfirmation = false;

  selectedProduct!: Product;

  private productService = inject(ProductService);

  private confirmationService = inject(ConfirmationService);

  private messageService = inject(MessageService);

  ngOnInit() {
    this.productService.getProductsSmall().then((products) => this.products.set(products));

    this.images = [];
    this.images.push({
      source: 'assets/demo/images/sopranos/sopranos1.jpg',
      thumbnail: 'assets/demo/images/sopranos/sopranos1_small.jpg',
      title: 'Sopranos 1'
    });
    this.images.push({
      source: 'assets/demo/images/sopranos/sopranos2.jpg',
      thumbnail: 'assets/demo/images/sopranos/sopranos2_small.jpg',
      title: 'Sopranos 2'
    });
    this.images.push({
      source: 'assets/demo/images/sopranos/sopranos3.jpg',
      thumbnail: 'assets/demo/images/sopranos/sopranos3_small.jpg',
      title: 'Sopranos 3'
    });
    this.images.push({
      source: 'assets/demo/images/sopranos/sopranos4.jpg',
      thumbnail: 'assets/demo/images/sopranos/sopranos4_small.jpg',
      title: 'Sopranos 4'
    });
  }

  confirm(event: Event) {
    this.confirmationService.confirm({
      key: 'confirm2',
      target: event.target || new EventTarget(),
      message: 'Are you sure that you want to proceed?',
      icon: 'pi pi-exclamation-triangle',
      accept: () => {
        this.messageService.add({ severity: 'info', summary: 'Confirmed', detail: 'You have accepted' });
      },
      reject: () => {
        this.messageService.add({ severity: 'error', summary: 'Rejected', detail: 'You have rejected' });
      }
    });
  }

  open() {
    this.display = true;
  }

  close() {
    this.display = false;
  }

  toggleDataTable(op: Popover, event: Event) {
    op.toggle(event);
  }

  onProductSelect(op: Popover, event: TableRowSelectEvent<Product>) {
    op.hide();

    const product = event.data as Product;

    this.messageService.add({ severity: 'info', summary: 'Product Selected', detail: product.name, life: 3000 });
  }

  openConfirmation() {
    this.displayConfirmation = true;
  }

  closeConfirmation() {
    this.displayConfirmation = false;
  }
}
