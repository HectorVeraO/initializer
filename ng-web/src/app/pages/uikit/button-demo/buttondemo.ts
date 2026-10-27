import { Component, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { MenuItem } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { ButtonGroupModule } from '@openng/optimus-ui/buttongroup';
import { SplitButtonModule } from '@openng/optimus-ui/splitbutton';

@Component({
  selector: 'p-button-demo',
  standalone: true,
  imports: [ButtonModule, ButtonGroupModule, SplitButtonModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './buttondemo.html'
})
export class ButtonDemo implements OnInit {
  items: MenuItem[] = [];

  readonly loading = signal([false, false, false, false]);

  ngOnInit() {
    this.items = [{ label: 'Update', icon: 'pi pi-refresh' }, { label: 'Delete', icon: 'pi pi-times' }, { label: 'Angular.io', icon: 'pi pi-info', url: 'http://angular.io' }, { separator: true }, { label: 'Setup', icon: 'pi pi-cog' }];
  }

  load(index: number) {
    this.setLoading(index, true);
    setTimeout(() => this.setLoading(index, false), 1000);
  }

  private setLoading(index: number, isLoading: boolean): void {
    this.loading.update((states) => states.map((state, position) => (position === index ? isLoading : state)));
  }
}
