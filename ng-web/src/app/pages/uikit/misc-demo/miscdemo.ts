import { Component, OnInit, OnDestroy, ChangeDetectionStrategy, signal } from '@angular/core';
import { AvatarModule } from '@openng/optimus-ui/avatar';
import { AvatarGroupModule } from '@openng/optimus-ui/avatargroup';
import { BadgeModule } from '@openng/optimus-ui/badge';
import { ButtonModule } from '@openng/optimus-ui/button';
import { ChipModule } from '@openng/optimus-ui/chip';
import { OverlayBadgeModule } from '@openng/optimus-ui/overlaybadge';
import { ProgressBarModule } from '@openng/optimus-ui/progressbar';
import { ScrollPanelModule } from '@openng/optimus-ui/scrollpanel';
import { ScrollTopModule } from '@openng/optimus-ui/scrolltop';
import { SkeletonModule } from '@openng/optimus-ui/skeleton';
import { TagModule } from '@openng/optimus-ui/tag';

@Component({
  selector: 'p-misc-demo',
  standalone: true,
  imports: [ProgressBarModule, BadgeModule, AvatarModule, ScrollPanelModule, TagModule, ChipModule, ButtonModule, SkeletonModule, AvatarGroupModule, ScrollTopModule, OverlayBadgeModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './miscdemo.html'
})
export class MiscDemo implements OnInit, OnDestroy {
  readonly value = signal(0);

  interval: number | undefined = undefined;

  ngOnInit() {
    this.interval = window.setInterval(() => {
      this.value.update((current) => Math.min(current + Math.floor(Math.random() * 10) + 1, 100));
      if (this.value() >= 100) {
        clearInterval(this.interval);
      }
    }, 2000);
  }

  ngOnDestroy() {
    clearInterval(this.interval);
  }
}
