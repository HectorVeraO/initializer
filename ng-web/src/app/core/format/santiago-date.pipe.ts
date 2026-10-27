import { Pipe, PipeTransform } from '@angular/core';
import { formatSantiago } from './santiago-date';

@Pipe({ name: 'santiagoDate', standalone: true })
export class SantiagoDatePipe implements PipeTransform {
  transform(value: string): string {
    return formatSantiago(value);
  }
}
