import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TranslocoModule } from '@jsverse/transloco';
import { environment } from '../../environments/environment';

/** Landing page of the console shell; outside production it links to the component demos. */
@Component({
  selector: 'p-home',
  standalone: true,
  imports: [RouterLink, TranslocoModule],
  templateUrl: './home.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class HomeComponent {
  readonly showDemos = !environment.production;

  readonly demos = [
    { id: 'dashboard', label: 'Dashboard', path: '/dev/dashboard' },
    { id: 'formlayout', label: 'Form Layout', path: '/dev/uikit/formlayout' },
    { id: 'input', label: 'Input', path: '/dev/uikit/input' },
    { id: 'button', label: 'Button', path: '/dev/uikit/button' },
    { id: 'table', label: 'Table', path: '/dev/uikit/table' },
    { id: 'list', label: 'List', path: '/dev/uikit/list' },
    { id: 'tree', label: 'Tree', path: '/dev/uikit/tree' },
    { id: 'panel', label: 'Panel', path: '/dev/uikit/panel' },
    { id: 'overlay', label: 'Overlay', path: '/dev/uikit/overlay' },
    { id: 'media', label: 'Media', path: '/dev/uikit/media' },
    { id: 'menu', label: 'Menu', path: '/dev/uikit/menu' },
    { id: 'message', label: 'Message', path: '/dev/uikit/message' },
    { id: 'file', label: 'File', path: '/dev/uikit/file' },
    { id: 'charts', label: 'Chart', path: '/dev/uikit/charts' },
    { id: 'timeline', label: 'Timeline', path: '/dev/uikit/timeline' },
    { id: 'misc', label: 'Misc', path: '/dev/uikit/misc' }
  ];
}
