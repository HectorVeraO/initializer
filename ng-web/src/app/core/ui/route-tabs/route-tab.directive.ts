import { Directive, TemplateRef, inject, input } from '@angular/core';

/**
 * Marks the content of one tab of `p-route-tabs`: `<ng-template pRouteTab="identity">...</ng-template>`.
 * The template is created the first time its tab is activated and kept afterwards.
 * Inside `@for`, bind the id: `<ng-template [pRouteTab]="group.prefix">`.
 */
@Directive({ selector: 'ng-template[pRouteTab]', standalone: true })
export class RouteTabDirective {
  readonly id = input.required<string>({ alias: 'pRouteTab' });
  readonly template = inject<TemplateRef<unknown>>(TemplateRef);
}
