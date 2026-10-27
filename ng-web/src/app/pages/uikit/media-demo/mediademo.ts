import { CommonModule } from '@angular/common';
import { Component, inject, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { ButtonModule } from '@openng/optimus-ui/button';
import { CarouselModule, CarouselResponsiveOptions } from '@openng/optimus-ui/carousel';
import { GalleriaModule } from '@openng/optimus-ui/galleria';
import { ImageModule } from '@openng/optimus-ui/image';
import { TagModule } from '@openng/optimus-ui/tag';
import { PhotoService } from '../../service/photo.service';
import { Product, ProductService } from '../../service/product.service';

interface GalleriaResponsiveOption {
  breakpoint: string;
  numVisible: number;
}

@Component({
  selector: 'p-media-demo',
  standalone: true,
  imports: [CommonModule, CarouselModule, ButtonModule, GalleriaModule, ImageModule, TagModule],
  templateUrl: './mediademo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [ProductService, PhotoService]
})
export class MediaDemo implements OnInit {
  readonly products = signal<Product[]>([]);

  readonly images = signal<Record<string, unknown>[]>([]);

  galleriaResponsiveOptions: GalleriaResponsiveOption[] = [
    {
      breakpoint: '1024px',
      numVisible: 5
    },
    {
      breakpoint: '960px',
      numVisible: 4
    },
    {
      breakpoint: '768px',
      numVisible: 3
    },
    {
      breakpoint: '560px',
      numVisible: 1
    }
  ];

  carouselResponsiveOptions: CarouselResponsiveOptions[] = [
    {
      breakpoint: '1024px',
      numVisible: 3,
      numScroll: 3
    },
    {
      breakpoint: '768px',
      numVisible: 2,
      numScroll: 2
    },
    {
      breakpoint: '560px',
      numVisible: 1,
      numScroll: 1
    }
  ];

  private productService = inject(ProductService);

  private photoService = inject(PhotoService);

  ngOnInit() {
    this.productService.getProductsSmall().then((products) => {
      this.products.set(products);
    });

    this.photoService.getImages().then((images) => {
      this.images.set(images);
    });
  }

  getSeverity(status: string) {
    switch (status) {
      case 'INSTOCK':
        return 'success';
      case 'LOWSTOCK':
        return 'warn';
      case 'OUTOFSTOCK':
        return 'danger';
      default:
        return 'success';
    }
  }
}
