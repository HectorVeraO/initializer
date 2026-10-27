import { Component, inject, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { ButtonModule } from '@openng/optimus-ui/button';

import { FormsModule } from '@angular/forms';
import { CheckboxModule } from '@openng/optimus-ui/checkbox';
import { RadioButtonModule } from '@openng/optimus-ui/radiobutton';
import { SelectButtonModule } from '@openng/optimus-ui/selectbutton';
import { InputGroupModule } from '@openng/optimus-ui/inputgroup';
import { FluidModule } from '@openng/optimus-ui/fluid';
import { IconFieldModule } from '@openng/optimus-ui/iconfield';
import { InputIconModule } from '@openng/optimus-ui/inputicon';
import { FloatLabelModule } from '@openng/optimus-ui/floatlabel';
import { AutoCompleteCompleteEvent, AutoCompleteModule } from '@openng/optimus-ui/autocomplete';
import { InputNumberModule } from '@openng/optimus-ui/inputnumber';
import { SliderModule } from '@openng/optimus-ui/slider';
import { RatingModule } from '@openng/optimus-ui/rating';
import { ColorPickerModule } from '@openng/optimus-ui/colorpicker';
import { KnobModule } from '@openng/optimus-ui/knob';
import { SelectModule } from '@openng/optimus-ui/select';
import { DatePickerModule } from '@openng/optimus-ui/datepicker';
import { ToggleSwitchModule } from '@openng/optimus-ui/toggleswitch';
import { TreeSelectModule } from '@openng/optimus-ui/treeselect';
import { MultiSelectModule } from '@openng/optimus-ui/multiselect';
import { ListboxModule } from '@openng/optimus-ui/listbox';
import { InputGroupAddonModule } from '@openng/optimus-ui/inputgroupaddon';
import { TextareaModule } from '@openng/optimus-ui/textarea';
import { ToggleButtonModule } from '@openng/optimus-ui/togglebutton';
import { CountryService } from '../../service/country.service';
import { NodeService } from '../../service/node.service';
import { TreeNode } from '@openng/optimus-ui/api';
import { Country } from '../../service/customer.service';

@Component({
  selector: 'p-input-demo',
  standalone: true,
  imports: [
    FormsModule,
    InputTextModule,
    ButtonModule,
    CheckboxModule,
    RadioButtonModule,
    SelectButtonModule,
    InputGroupModule,
    FluidModule,
    IconFieldModule,
    InputIconModule,
    FloatLabelModule,
    AutoCompleteModule,
    InputNumberModule,
    SliderModule,
    RatingModule,
    ColorPickerModule,
    KnobModule,
    SelectModule,
    DatePickerModule,
    ToggleButtonModule,
    ToggleSwitchModule,
    TreeSelectModule,
    MultiSelectModule,
    ListboxModule,
    InputGroupAddonModule,
    TextareaModule
  ],
  templateUrl: './inputdemo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [CountryService, NodeService]
})
export class InputDemo implements OnInit {
  floatValue: unknown = null;

  autoValue: Country[] | undefined;

  autoFilteredValue: Country[] = [];

  selectedAutoValue: Country | null = null;

  calendarValue: Date | null = null;

  inputNumberValue: number | null = null;

  sliderValue = 50;

  ratingValue: number | null = null;

  colorValue = '#1976D2';

  radioValue: string | null = null;

  checkboxValue: string[] = [];

  switchValue = false;

  listboxValues: Country[] = [
    { name: 'New York', code: 'NY' },
    { name: 'Rome', code: 'RM' },
    { name: 'London', code: 'LDN' },
    { name: 'Istanbul', code: 'IST' },
    { name: 'Paris', code: 'PRS' }
  ];

  listboxValue: Country | null = null;

  dropdownValues: Country[] = [
    { name: 'New York', code: 'NY' },
    { name: 'Rome', code: 'RM' },
    { name: 'London', code: 'LDN' },
    { name: 'Istanbul', code: 'IST' },
    { name: 'Paris', code: 'PRS' }
  ];

  dropdownValue: Country | null = null;

  multiselectCountries: Country[] = [
    { name: 'Australia', code: 'AU' },
    { name: 'Brazil', code: 'BR' },
    { name: 'China', code: 'CN' },
    { name: 'Egypt', code: 'EG' },
    { name: 'France', code: 'FR' },
    { name: 'Germany', code: 'DE' },
    { name: 'India', code: 'IN' },
    { name: 'Japan', code: 'JP' },
    { name: 'Spain', code: 'ES' },
    { name: 'United States', code: 'US' }
  ];

  multiselectSelectedCountries!: Country[];

  toggleValue = false;

  selectButtonValue: { name: string } | null = null;

  selectButtonValues: { name: string }[] = [{ name: 'Option 1' }, { name: 'Option 2' }, { name: 'Option 3' }];

  knobValue = 50;

  inputGroupValue = false;

  readonly treeSelectNodes = signal<TreeNode[]>([]);

  selectedNode: TreeNode | null = null;

  countryService = inject(CountryService);

  nodeService = inject(NodeService);

  ngOnInit() {
    this.countryService.getCountries().then((countries) => {
      this.autoValue = countries;
    });

    this.nodeService.getFiles().then((data) => this.treeSelectNodes.set(data));
  }

  filterCountry(event: AutoCompleteCompleteEvent) {
    const filtered: Country[] = [];

    const query = event.query;

    for (const country of this.autoValue ?? []) {
      if (country.name?.toLowerCase().indexOf(query.toLowerCase()) == 0) {
        filtered.push(country);
      }
    }

    this.autoFilteredValue = filtered;
  }
}
