import { Component, ChangeDetectionStrategy } from '@angular/core';
import { RouterModule } from '@angular/router';
import { ToastModule } from '@openng/optimus-ui/toast';

@Component({
  selector: 'p-root',
  standalone: true,
  imports: [RouterModule, ToastModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: 'app.component.html'
})
export class AppComponent {}
