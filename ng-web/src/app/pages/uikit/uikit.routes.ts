import { Routes } from '@angular/router';
import { ButtonDemo } from './button-demo/buttondemo';
import { ChartDemo } from './char-demo/chartdemo';
import { FileDemo } from './file-demo/filedemo';
import { FormLayoutDemo } from './form-layout-demo/formlayoutdemo';
import { InputDemo } from './input-demo/inputdemo';
import { ListDemo } from './list-demo/listdemo';
import { MediaDemo } from './media-demo/mediademo';
import { MessagesDemo } from './messages-demo/messagesdemo';
import { MiscDemo } from './misc-demo/miscdemo';
import { PanelsDemo } from './panels-demo/panelsdemo';
import { TimelineDemo } from './timeline-demo/timelinedemo';
import { TableDemo } from './table-demo/tabledemo';
import { OverlayDemo } from './overlay-demo/overlaydemo';
import { TreeDemo } from './tree-demo/treedemo';
import { MenuDemo } from './menu-demo/menudemo';

export default [
  { path: 'button', data: { breadcrumb: 'Button' }, component: ButtonDemo },
  { path: 'charts', data: { breadcrumb: 'Charts' }, component: ChartDemo },
  { path: 'file', data: { breadcrumb: 'File' }, component: FileDemo },
  { path: 'formlayout', data: { breadcrumb: 'Form Layout' }, component: FormLayoutDemo },
  { path: 'input', data: { breadcrumb: 'Input' }, component: InputDemo },
  { path: 'list', data: { breadcrumb: 'List' }, component: ListDemo },
  { path: 'media', data: { breadcrumb: 'Media' }, component: MediaDemo },
  { path: 'message', data: { breadcrumb: 'Message' }, component: MessagesDemo },
  { path: 'misc', data: { breadcrumb: 'Misc' }, component: MiscDemo },
  { path: 'panel', data: { breadcrumb: 'Panel' }, component: PanelsDemo },
  { path: 'timeline', data: { breadcrumb: 'Timeline' }, component: TimelineDemo },
  { path: 'table', data: { breadcrumb: 'Table' }, component: TableDemo },
  { path: 'overlay', data: { breadcrumb: 'Overlay' }, component: OverlayDemo },
  { path: 'tree', data: { breadcrumb: 'Tree' }, component: TreeDemo },
  { path: 'menu', data: { breadcrumb: 'Menu' }, component: MenuDemo },
  { path: '**', redirectTo: '/notfound' }
] as Routes;
