import { Component, ChangeDetectionStrategy } from '@angular/core';

@Component({
  standalone: true,
  selector: 'p-stats-widget',
  imports: [],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './statswidget.html'
})
export class StatsWidget {}
