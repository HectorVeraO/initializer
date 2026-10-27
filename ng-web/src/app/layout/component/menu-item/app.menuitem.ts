import { Component, HostBinding, Input, OnInit, OnDestroy, inject, ChangeDetectionStrategy, signal } from '@angular/core';
import { NavigationEnd, Router, RouterModule } from '@angular/router';
import { Subscription } from 'rxjs';
import { filter } from 'rxjs/operators';
import { CommonModule } from '@angular/common';
import { RippleModule } from '@openng/optimus-ui/ripple';
import { MenuItem } from '@openng/optimus-ui/api';
import { LayoutService } from '../../service/layout.service';

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: '[app-menuitem]',
  imports: [CommonModule, RouterModule, RippleModule],
  templateUrl: './app.menuitem.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [LayoutService]
})
export class AppMenuitem implements OnInit, OnDestroy {
  @Input() item!: MenuItem;

  @Input() index!: number;

  @Input() @HostBinding('class.layout-root-menuitem') root!: boolean;

  @Input() parentKey!: string;

  readonly active = signal(false);

  menuSourceSubscription: Subscription;

  menuResetSubscription: Subscription;

  routerEventsSubscription: Subscription;

  key = '';

  public router = inject(Router);
  private layoutService = inject(LayoutService);

  constructor() {
    this.menuSourceSubscription = this.layoutService.menuSource$.subscribe((value) => {
      Promise.resolve(null).then(() => {
        if (value.routeEvent) {
          this.active.set(value.key === this.key || value.key.startsWith(this.key + '-'));
        } else {
          if (value.key !== this.key && !value.key.startsWith(this.key + '-')) {
            this.active.set(false);
          }
        }
      });
    });

    this.menuResetSubscription = this.layoutService.resetSource$.subscribe(() => {
      this.active.set(false);
    });

    this.routerEventsSubscription = this.router.events.pipe(filter((event) => event instanceof NavigationEnd)).subscribe(() => {
      if (this.item.routerLink) {
        this.updateActiveStateFromRoute();
      }
    });
  }

  ngOnInit() {
    this.key = this.parentKey ? this.parentKey + '-' + this.index : String(this.index);

    if (this.item.routerLink) {
      this.updateActiveStateFromRoute();
    }
  }

  updateActiveStateFromRoute() {
    const activeRoute = this.router.isActive(this.item.routerLink[0], { paths: 'exact', queryParams: 'ignored', matrixParams: 'ignored', fragment: 'ignored' });

    if (activeRoute) {
      this.layoutService.onMenuStateChange({ key: this.key, routeEvent: true });
    }
  }

  itemClick(event: Event) {
    // avoid processing disabled items
    if (this.item.disabled) {
      event.preventDefault();

      return;
    }

    // execute command
    if (this.item.command) {
      this.item.command({ originalEvent: event, item: this.item });
    }

    // toggle active state
    if (this.item.items) {
      this.active.update((active) => !active);
    }

    this.layoutService.onMenuStateChange({ key: this.key });
  }

  /**
   * Whether the submenu is expanded. Root submenus are always expanded; the
   * height transition between states lives in `.layout-submenu-container`.
   */
  get submenuExpanded(): boolean {
    return this.root || this.active();
  }

  @HostBinding('class.active-menuitem')
  get activeClass() {
    return this.active() && !this.root;
  }

  ngOnDestroy() {
    if (this.menuSourceSubscription) {
      this.menuSourceSubscription.unsubscribe();
    }

    if (this.menuResetSubscription) {
      this.menuResetSubscription.unsubscribe();
    }

    if (this.routerEventsSubscription) {
      this.routerEventsSubscription.unsubscribe();
    }
  }
}
