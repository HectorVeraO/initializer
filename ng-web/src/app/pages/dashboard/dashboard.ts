import { Component, ChangeDetectionStrategy } from '@angular/core';
import { NotificationsWidget } from './components/notifications-widget/notificationswidget';
import { StatsWidget } from './components/stats-widget/statswidget';
import { BestSellingWidget } from './components/best-selling-widget/bestsellingwidget';
import { RevenueStreamWidget } from './components/renueve-stream-widget/revenuestreamwidget';

@Component({
  selector: 'p-dashboard',
  imports: [StatsWidget, BestSellingWidget, RevenueStreamWidget, NotificationsWidget],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <div class="grid grid-cols-12 gap-8">
      <p-stats-widget class="contents" />
      <div class="col-span-12 xl:col-span-6">
        <p-best-selling-widget />
      </div>
      <div class="col-span-12 xl:col-span-6">
        <p-revenue-stream-widget />
        <p-notifications-widget />
      </div>
    </div>
  `
})
export class Dashboard {}
