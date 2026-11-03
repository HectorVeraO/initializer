import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, contentChildren, effect, inject, input, output, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, NavigationEnd, Router } from '@angular/router';
import { BadgeModule } from '@openng/optimus-ui/badge';
import { TabsModule } from '@openng/optimus-ui/tabs';
import { filter } from 'rxjs';
import { RouteTabDirective } from './route-tab.directive';

/** One tab of `p-route-tabs`. `label` is already translated; `id` is the URL path segment. */
export interface RouteTab {
  id: string;
  label: string;
  icon?: string;
  badge?: string | number;
  disabled?: boolean;
}

/**
 * Tabs whose selection lives in the URL path: `/<page>/<tab>` (or `/<page>/<id>/<tab>`).
 *
 * The page route declares the segment as a child: `{ path: ':tab', children: [] }`
 * (the name is the `param` input). The content of each tab is a projected
 * `<ng-template pRouteTab="id">`, created the first time the tab is activated
 * and kept (hidden) afterwards, so a tab that was never opened requests nothing.
 *
 * A missing, unknown or disabled id in the URL is replaced (`replaceUrl`) by
 * `defaultTab`, else the first enabled tab. Browser back/forward and reload keep
 * the tab because the URL is the single source of truth. Keyboard (arrows, Home,
 * End, Enter) and the `tab`/`tabpanel` roles come from the library tabs.
 *
 * `tabFirstActivated` fires once per tab id, then `tabActivated` fires on every activation.
 * The library renders an `autofocus` attribute on selects and buttons that never asked for it,
 * and the browser honors it on the first load (focus ring, page scrolled to the paginator).
 * The attribute is removed from everything inside the panels as soon as it appears.
 * Shared filters stay outside, above this block; use `tabActivated` to reload stale data.
 */
@Component({
  selector: 'p-route-tabs',
  standalone: true,
  imports: [NgTemplateOutlet, TabsModule, BadgeModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block min-w-0' },
  template: `
    @if (tabs().length > 0) {
      <p-tabs [value]="activeId() ?? undefined" [scrollable]="true" (valueChange)="select($event)">
        <p-tablist>
          @for (tab of tabs(); track tab.id) {
            <p-tab [value]="tab.id" [disabled]="tab.disabled === true" [attr.data-testid]="testIdPrefix() + tab.id">
              @if (tab.icon) {
                <i [class]="tab.icon" aria-hidden="true"></i>
              }
              <span>{{ tab.label }}</span>
              @if (tab.badge !== undefined) {
                <p-badge [value]="tab.badge" severity="secondary" badgeSize="small" />
              }
            </p-tab>
          }
        </p-tablist>
        <p-tabpanels>
          @for (tab of tabs(); track tab.id) {
            <p-tabpanel [value]="tab.id" [attr.data-testid]="'route-tabpanel-' + tab.id">
              @if (isCreated(tab.id)) {
                <ng-container [ngTemplateOutlet]="templateOf(tab.id)" />
              }
            </p-tabpanel>
          }
        </p-tabpanels>
      </p-tabs>
    }
  `
})
export class RouteTabsComponent {
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  readonly tabs = input.required<readonly RouteTab[]>();
  /** Name of the route parameter that carries the tab id. */
  readonly param = input('tab');
  /** Tab shown when the URL names none or an unusable one; the first enabled tab when absent. */
  readonly defaultTab = input<string | undefined>(undefined);
  /** Prefix of the `data-testid` of each tab button (`<prefix><id>`). */
  readonly testIdPrefix = input('route-tab-');

  /** Every activation of a tab, including the first. */
  readonly tabActivated = output<string>();
  /** The first activation of a tab id; emitted before `tabActivated`. */
  readonly tabFirstActivated = output<string>();

  private readonly contents = contentChildren(RouteTabDirective, { descendants: true });
  private readonly activated = signal<ReadonlySet<string>>(new Set<string>());

  private readonly navigation = toSignal(this.router.events.pipe(filter((event) => event instanceof NavigationEnd)), { initialValue: null });

  /** The tab id in the URL, or null when the URL names none. */
  readonly requestedId = computed(() => {
    this.navigation();

    return this.readParam(this.param());
  });

  /** The tab shown: the requested one when usable, else the default, else the first enabled one. */
  readonly activeId = computed(() => {
    const usable = this.tabs().filter((tab) => tab.disabled !== true);

    const requested = usable.find((tab) => tab.id === this.requestedId());

    const preferred = usable.find((tab) => tab.id === this.defaultTab());

    const chosen = requested ?? preferred ?? usable[0];

    return chosen?.id ?? null;
  });

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private hasReleasedAutofocus = false;

  constructor() {
    const observer = new MutationObserver(() => this.stripAutofocus());

    observer.observe(this.host.nativeElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['autofocus'] });
    inject(DestroyRef).onDestroy(() => observer.disconnect());

    effect(() => {
      const active = this.activeId();

      if (active === null) {
        return;
      }

      untracked(() => this.activate(active));
    });

    effect(() => {
      const active = this.activeId();

      const isMisplaced = active !== null && active !== this.requestedId();

      if (isMisplaced) {
        void this.router.navigate([active], { relativeTo: this.route, replaceUrl: true, queryParamsHandling: 'preserve' });
      }
    });
  }

  /**
   * The browser queues a control for autofocus when its attribute is set and decides at the next
   * rendering step, so on the first sighting the panels are made inert (not focusable) until that
   * step has passed. The library sets the attribute again on every change detection, so the
   * attribute is only stripped afterwards.
   */
  private stripAutofocus(): void {
    const marked = this.host.nativeElement.querySelectorAll('[role="tabpanel"] [autofocus]');

    if (marked.length === 0) {
      return;
    }

    marked.forEach((element) => element.removeAttribute('autofocus'));

    if (this.hasReleasedAutofocus) {
      return;
    }

    this.hasReleasedAutofocus = true;

    const panels = this.host.nativeElement.querySelectorAll('[role="tabpanel"]');

    panels.forEach((panel) => panel.setAttribute('inert', ''));
    requestAnimationFrame(() => requestAnimationFrame(() => panels.forEach((panel) => panel.removeAttribute('inert'))));
  }

  /** True once the tab was activated: its content exists and stays. */
  protected isCreated(id: string): boolean {
    return id === this.activeId() || this.activated().has(id);
  }

  protected templateOf(id: string): RouteTabDirective['template'] | null {
    const match = this.contents().find((content) => content.id() === id);

    return match?.template ?? null;
  }

  /** A tab was chosen (click or keyboard): the URL carries the choice. */
  protected select(value: string | number | undefined): void {
    const id = value === undefined ? null : String(value);

    const isNewChoice = id !== null && id !== this.activeId();

    if (isNewChoice) {
      void this.router.navigate([id], { relativeTo: this.route, queryParamsHandling: 'preserve' });
    }
  }

  private activate(id: string): void {
    const isFirst = !this.activated().has(id);

    if (isFirst) {
      this.activated.update((current) => new Set(current).add(id));
      this.tabFirstActivated.emit(id);
    }

    this.tabActivated.emit(id);
  }

  private readParam(name: string): string | null {
    let current: ActivatedRoute | null = this.route.firstChild;

    while (current !== null) {
      const value = current.snapshot?.paramMap.get(name) ?? null;

      if (value !== null) {
        return value;
      }

      current = current.firstChild;
    }

    return null;
  }
}
