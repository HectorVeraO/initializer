import { Component, ElementRef, inject, ChangeDetectionStrategy } from '@angular/core';
import { AppMenu } from '../menu/app.menu';

@Component({
  selector: 'p-sidebar',
  standalone: true,
  imports: [AppMenu],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './app.sidebar.html'
})
export class AppSidebar {
  public el = inject(ElementRef);
}
