import { Pipe, PipeTransform } from '@angular/core';

const integerFormat = new Intl.NumberFormat('es-CL', { maximumFractionDigits: 0 });

const costFormat = new Intl.NumberFormat('es-CL', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** Chilean number format (`1.234`, `1.234,50`), independent of the locale the app was bootstrapped with. */
@Pipe({ name: 'clNumber', standalone: true })
export class ClNumberPipe implements PipeTransform {
  transform(value: number | null | undefined, kind: 'int' | 'cost' = 'int'): string {
    if (value === null || value === undefined) {
      return '';
    }

    return kind === 'cost' ? costFormat.format(value) : integerFormat.format(value);
  }
}
