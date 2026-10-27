import { Component, OnInit, ChangeDetectionStrategy } from '@angular/core';
import { TimelineModule } from '@openng/optimus-ui/timeline';
import { CardModule } from '@openng/optimus-ui/card';
import { CommonModule } from '@angular/common';
import { ButtonModule } from '@openng/optimus-ui/button';

@Component({
  selector: 'p-timeline-demo',
  standalone: true,
  imports: [CommonModule, TimelineModule, ButtonModule, CardModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './timelinedemo.html'
})
export class TimelineDemo implements OnInit {
  events1: Record<string, unknown>[] = [];

  events2: string[] = [];

  ngOnInit() {
    this.events1 = [
      {
        status: 'Ordered',
        date: '15/10/2020 10:30',
        icon: 'pi pi-shopping-cart',
        color: '#9C27B0',
        image: 'game-controller.jpg'
      },
      {
        status: 'Processing',
        date: '15/10/2020 14:00',
        icon: 'pi pi-cog',
        color: '#673AB7'
      },
      {
        status: 'Shipped',
        date: '15/10/2020 16:15',
        icon: 'pi pi-envelope',
        color: '#FF9800'
      },
      {
        status: 'Delivered',
        date: '16/10/2020 10:00',
        icon: 'pi pi-check',
        color: '#607D8B'
      }
    ];

    this.events2 = ['2020', '2021', '2022', '2023'];
  }
}
