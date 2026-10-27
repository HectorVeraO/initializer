import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ButtonModule } from '@openng/optimus-ui/button';
import { MenuModule } from '@openng/optimus-ui/menu';

@Component({
  standalone: true,
  selector: 'p-notifications-widget',
  imports: [ButtonModule, MenuModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './notificationswidget.html'
})
export class NotificationsWidget {
  items = [
    { label: 'Add New', icon: 'pi pi-fw pi-plus' },
    { label: 'Remove', icon: 'pi pi-fw pi-trash' }
  ];
}
