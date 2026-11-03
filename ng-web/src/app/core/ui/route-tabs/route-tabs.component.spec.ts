import { Component, OnInit, input, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { byTestId, configureFeatureTest, settle, stubResizeObserver } from '@/core/testing/ui-testing';
import { RouteTabDirective } from './route-tab.directive';
import { RouteTab, RouteTabsComponent } from './route-tabs.component';

/** Ids of the tab contents created so far, in creation order. */
const created: string[] = [];

@Component({
  selector: 'p-probe',
  standalone: true,
  template: `<span [attr.data-testid]="'content-' + tabId()">content {{ tabId() }}</span>`
})
class ProbeComponent implements OnInit {
  readonly tabId = input.required<string>();

  ngOnInit(): void {
    created.push(this.tabId());
  }
}

@Component({
  selector: 'p-route-tabs-host',
  standalone: true,
  imports: [RouteTabsComponent, RouteTabDirective, ProbeComponent],
  template: `
    <p-route-tabs [tabs]="tabs()" [defaultTab]="defaultTab()" testIdPrefix="tab-" (tabActivated)="activated.push($event)" (tabFirstActivated)="firstActivated.push($event)">
      @for (tab of tabs(); track tab.id) {
        <ng-template [pRouteTab]="tab.id"><p-probe [tabId]="tab.id" /></ng-template>
      }
    </p-route-tabs>
  `
})
class HostComponent {
  readonly tabs = signal<RouteTab[]>([
    { id: 'a', label: 'Alpha' },
    { id: 'b', label: 'Beta' },
    { id: 'c', label: 'Gamma', disabled: true },
    { id: 'd', label: 'Delta' }
  ]);

  readonly defaultTab = signal<string | undefined>(undefined);
  readonly activated: string[] = [];
  readonly firstActivated: string[] = [];
}

/** The page route declares the tab as a child segment, with or without an id before it. */
const ROUTES = [
  { path: 'page', component: HostComponent, children: [{ path: ':tab', children: [] }] },
  { path: 'item/:id', component: HostComponent, children: [{ path: ':tab', children: [] }] }
];

describe('RouteTabsComponent', () => {
  let fixture: ComponentFixture<unknown>;

  let host: HostComponent;

  async function open(url: string): Promise<void> {
    stubResizeObserver();
    created.length = 0;

    const harness = await RouterTestingHarness.create();

    host = await harness.navigateByUrl(url, HostComponent);
    fixture = harness.fixture;
    await settle(fixture);
  }

  function root(): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function tabButton(id: string): HTMLElement {
    return byTestId(root(), `tab-${id}`) as HTMLElement;
  }

  function currentUrl(): string {
    return TestBed.inject(Router).url;
  }

  async function click(id: string): Promise<void> {
    tabButton(id).click();
    await settle(fixture);
  }

  async function goTo(url: string): Promise<void> {
    await TestBed.inject(Router).navigateByUrl(url);
    await settle(fixture);
  }

  function press(element: HTMLElement, key: string): void {
    element.dispatchEvent(new KeyboardEvent('keydown', { key, code: key, bubbles: true, cancelable: true }));
  }

  /** Test ids of the contents inside the panel that is not hidden. */
  function visibleContent(): string[] {
    const panels = Array.from(root().querySelectorAll('[role="tabpanel"]')).filter((panel) => !panel.hasAttribute('hidden'));

    return panels.flatMap((panel) => Array.from(panel.querySelectorAll('[data-testid^="content-"]')).map((node) => node.getAttribute('data-testid') as string));
  }

  beforeEach(() => {
    configureFeatureTest(ROUTES);
  });

  it('creates only the tab named in the URL and reports its first activation', async () => {
    await open('/page/b');

    expect(created).toEqual(['b']);
    expect(visibleContent()).toEqual(['content-b']);
    expect(host.firstActivated).toEqual(['b']);
    expect(host.activated).toEqual(['b']);
    expect(tabButton('b').getAttribute('aria-selected')).toBe('true');
    expect(tabButton('a').getAttribute('aria-selected')).toBe('false');
  });

  it('moves the URL on click, creates a tab once and keeps it when the person returns', async () => {
    await open('/page/a');

    await click('b');

    expect(currentUrl()).toBe('/page/b');
    expect(visibleContent()).toEqual(['content-b']);

    await click('a');

    expect(currentUrl()).toBe('/page/a');
    expect(visibleContent()).toEqual(['content-a']);
    expect(created).toEqual(['a', 'b']);
    expect(host.firstActivated).toEqual(['a', 'b']);
    expect(host.activated).toEqual(['a', 'b', 'a']);
  });

  it('replaces a missing, unknown or disabled id by the first enabled tab, or by the default tab when one is given', async () => {
    await open('/page');

    expect(currentUrl()).toBe('/page/a');

    await goTo('/page/zzz');

    expect(currentUrl()).toBe('/page/a');
    expect(visibleContent()).toEqual(['content-a']);

    await goTo('/page/c');

    expect(currentUrl()).toBe('/page/a');

    host.defaultTab.set('d');
    await goTo('/page/yyy');

    expect(currentUrl()).toBe('/page/d');
    expect(visibleContent()).toEqual(['content-d']);
  });

  it('follows a URL change such as browser back or forward, and a reload shows the tab of the URL', async () => {
    await open('/page/a');
    await click('b');
    await click('d');
    await goTo('/page/b');

    expect(visibleContent()).toEqual(['content-b']);
    expect(tabButton('b').getAttribute('aria-selected')).toBe('true');
    expect(host.firstActivated).toEqual(['a', 'b', 'd']);
    expect(host.activated).toEqual(['a', 'b', 'd', 'b']);
  });

  it('works under an id in the path and never selects a disabled tab', async () => {
    await open('/item/42/b');

    expect(visibleContent()).toEqual(['content-b']);

    await click('c');

    expect(currentUrl()).toBe('/item/42/b');

    await click('d');

    expect(currentUrl()).toBe('/item/42/d');
  });

  it('moves focus with the arrow keys, Home and End, and activates the focused tab with Enter', async () => {
    await open('/page/a');

    press(tabButton('a'), 'ArrowRight');
    await settle(fixture);

    expect(document.activeElement).toBe(tabButton('b'));

    press(tabButton('b'), 'End');
    await settle(fixture);

    expect(document.activeElement).toBe(tabButton('d'));
    expect(currentUrl()).toBe('/page/a');

    press(tabButton('d'), 'Enter');
    await settle(fixture);

    expect(currentUrl()).toBe('/page/d');

    press(tabButton('d'), 'Home');
    await settle(fixture);

    expect(document.activeElement).toBe(tabButton('a'));
  });
});
