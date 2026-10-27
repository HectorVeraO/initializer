import { Component, ChangeDetectionStrategy } from '@angular/core';
import { FluidModule } from '@openng/optimus-ui/fluid';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { ButtonModule } from '@openng/optimus-ui/button';
import { SelectModule } from '@openng/optimus-ui/select';
import { FormsModule } from '@angular/forms';
import { TextareaModule } from '@openng/optimus-ui/textarea';

@Component({
  selector: 'p-formlayout-demo',
  standalone: true,
  imports: [InputTextModule, FluidModule, ButtonModule, SelectModule, FormsModule, TextareaModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './formlayoutdemo.html'
})
export class FormLayoutDemo {
  dropdownItems = [
    { name: 'Option 1', code: 'Option 1' },
    { name: 'Option 2', code: 'Option 2' },
    { name: 'Option 3', code: 'Option 3' }
  ];

  dropdownItem = null;
}
