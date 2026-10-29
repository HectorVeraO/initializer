import { Component, ChangeDetectionStrategy } from '@angular/core';
import { RouterModule } from '@angular/router';
import { TranslocoModule } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { BRAND } from '@/core/config/brand';

@Component({
  selector: 'p-notfound',
  standalone: true,
  imports: [RouterModule, TranslocoModule, ButtonModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './notfound.html'
})
export class Notfound {
  protected readonly brand = BRAND;
}
