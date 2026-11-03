import { Component, Renderer2, OnDestroy, computed, inject, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { NavigationEnd, Router, RouterModule } from '@angular/router';
import { Subject, filter } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
import { FLAG_SOURCE, UI_RESTYLE_FLAG } from '@/platform/core';
import { AppTopbar } from '../topbar/app.topbar';
import { AppTopbarLegacy } from '../topbar/app.topbar-legacy';
import { AppSidebar } from '../side-bar/app.sidebar';
import { AppFooter } from '../footer/app.footer';
import { LayoutService } from '../../service/layout.service';
import { TranslocoModule } from '@jsverse/transloco';
import { ConfirmDialog } from '@openng/optimus-ui/confirmdialog';
import { ConfirmNameDialogComponent } from '@/core/ui/confirm-name-dialog/confirm-name-dialog.component';

// The flag lives in the shell contract so every screen can name the same key; the shell reads it once, here.
export { UI_RESTYLE_FLAG };

@Component({
  selector: 'p-layout',
  standalone: true,
  imports: [CommonModule, AppTopbar, AppTopbarLegacy, AppSidebar, RouterModule, AppFooter, ConfirmNameDialogComponent, ConfirmDialog, TranslocoModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './app.layout.html'
})
export class AppLayout implements OnDestroy {
  private readonly destroy$ = new Subject<void>();

  menuOutsideClickListener: (() => void) | null = null;

  public layoutService = inject(LayoutService);
  public renderer = inject(Renderer2);
  public router = inject(Router);

  private readonly flags = inject(FLAG_SOURCE);

  /** Evaluated at the shell boundary only, and follows the session: the frame switches when the flag source does. */
  protected readonly restyled = computed(() => this.flags.isEnabled(UI_RESTYLE_FLAG));

  constructor() {
    this.layoutService.overlayOpen$.pipe(takeUntil(this.destroy$)).subscribe(() => {
      if (!this.menuOutsideClickListener) {
        this.menuOutsideClickListener = this.renderer.listen('document', 'click', (event) => {
          if (this.isOutsideClicked(event)) {
            this.hideMenu();
          }
        });
      }

      if (this.layoutService.layoutState().staticMenuMobileActive) {
        this.blockBodyScroll();
      }
    });

    this.router.events
      .pipe(
        filter((event) => event instanceof NavigationEnd),
        takeUntil(this.destroy$)
      )
      .subscribe(() => {
        this.hideMenu();
      });
  }

  isOutsideClicked(event: MouseEvent) {
    const sidebarEl = document.querySelector('.layout-sidebar');

    const topbarEl = document.querySelector('.layout-menu-button');

    const eventTarget = event.target as Node;

    return !(sidebarEl?.isSameNode(eventTarget) || sidebarEl?.contains(eventTarget) || topbarEl?.isSameNode(eventTarget) || topbarEl?.contains(eventTarget));
  }

  hideMenu() {
    this.layoutService.layoutState.update((prev) => ({ ...prev, overlayMenuActive: false, staticMenuMobileActive: false, menuHoverActive: false }));
    if (this.menuOutsideClickListener) {
      this.menuOutsideClickListener();
      this.menuOutsideClickListener = null;
    }
    this.unblockBodyScroll();
  }

  blockBodyScroll(): void {
    if (document.body.classList) {
      document.body.classList.add('blocked-scroll');
    } else {
      document.body.className += ' blocked-scroll';
    }
  }

  unblockBodyScroll(): void {
    if (document.body.classList) {
      document.body.classList.remove('blocked-scroll');
    } else {
      document.body.className = document.body.className.replace(new RegExp('(^|\\b)' + 'blocked-scroll'.split(' ').join('|') + '(\\b|$)', 'gi'), ' ');
    }
  }

  get containerClass() {
    return {
      'layout-overlay': this.layoutService.layoutConfig().menuMode === 'overlay',
      'layout-static': this.layoutService.layoutConfig().menuMode === 'static',
      'layout-static-inactive': this.layoutService.layoutState().staticMenuDesktopInactive && this.layoutService.layoutConfig().menuMode === 'static',
      'layout-overlay-active': this.layoutService.layoutState().overlayMenuActive,
      'layout-mobile-active': this.layoutService.layoutState().staticMenuMobileActive
    };
  }

  ngOnDestroy() {
    this.destroy$.next();
    this.destroy$.complete();

    if (this.menuOutsideClickListener) {
      this.menuOutsideClickListener();
    }
  }
}
