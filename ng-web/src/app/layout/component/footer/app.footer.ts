import { Component, ChangeDetectionStrategy } from '@angular/core';
import { TranslocoModule } from '@jsverse/transloco';

@Component({
  standalone: true,
  selector: 'p-footer',
  imports: [TranslocoModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './app.footer.html'
})
export class AppFooter {
  readonly year = new Date().getFullYear();
}
