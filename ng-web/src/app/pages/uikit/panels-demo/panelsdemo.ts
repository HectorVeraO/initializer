import { CommonModule } from '@angular/common';
import { Component, ChangeDetectionStrategy } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AccordionModule } from '@openng/optimus-ui/accordion';
import { MenuItem } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DividerModule } from '@openng/optimus-ui/divider';
import { FieldsetModule } from '@openng/optimus-ui/fieldset';
import { IconFieldModule } from '@openng/optimus-ui/iconfield';
import { InputIconModule } from '@openng/optimus-ui/inputicon';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MenuModule } from '@openng/optimus-ui/menu';
import { PanelModule } from '@openng/optimus-ui/panel';
import { RippleModule } from '@openng/optimus-ui/ripple';
import { SplitButtonModule } from '@openng/optimus-ui/splitbutton';
import { SplitterModule } from '@openng/optimus-ui/splitter';
import { TabsModule } from '@openng/optimus-ui/tabs';
import { ToolbarModule } from '@openng/optimus-ui/toolbar';

@Component({
  selector: 'p-panels-demo',
  standalone: true,
  imports: [
    CommonModule,
    FormsModule,
    ToolbarModule,
    ButtonModule,
    RippleModule,
    SplitButtonModule,
    AccordionModule,
    FieldsetModule,
    MenuModule,
    InputTextModule,
    DividerModule,
    SplitterModule,
    PanelModule,
    TabsModule,
    IconFieldModule,
    InputIconModule
  ],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './panelsdemo.html'
})
export class PanelsDemo {
  items: MenuItem[] = [
    {
      label: 'Save',
      icon: 'pi pi-check'
    },
    {
      label: 'Update',
      icon: 'pi pi-upload'
    },
    {
      label: 'Delete',
      icon: 'pi pi-trash'
    },
    {
      label: 'Home Page',
      icon: 'pi pi-home'
    }
  ];
}
