import { Component, inject, OnInit, OnDestroy, ChangeDetectionStrategy, signal } from '@angular/core';
import { ChartModule } from '@openng/optimus-ui/chart';
import { FluidModule } from '@openng/optimus-ui/fluid';
import { debounceTime, Subscription } from 'rxjs';
import { LayoutService } from '../../../layout/service/layout.service';

@Component({
  selector: 'p-chart-demo',
  standalone: true,
  imports: [ChartModule, FluidModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './chartdemo.html'
})
export class ChartDemo implements OnInit, OnDestroy {
  readonly lineData = signal<Record<string, unknown>>({});

  readonly barData = signal<Record<string, unknown>>({});

  readonly pieData = signal<Record<string, unknown>>({});

  readonly polarData = signal<Record<string, unknown>>({});

  readonly radarData = signal<Record<string, unknown>>({});

  readonly lineOptions = signal<Record<string, unknown>>({});

  readonly barOptions = signal<Record<string, unknown>>({});

  readonly pieOptions = signal<Record<string, unknown>>({});

  readonly polarOptions = signal<Record<string, unknown>>({});

  readonly radarOptions = signal<Record<string, unknown>>({});

  subscription: Subscription;

  layoutService = inject(LayoutService);

  constructor() {
    this.subscription = this.layoutService.configUpdate$.pipe(debounceTime(25)).subscribe(() => {
      this.initCharts();
    });
  }

  ngOnInit() {
    this.initCharts();
  }

  initCharts() {
    const documentStyle = getComputedStyle(document.documentElement);

    const textColor = documentStyle.getPropertyValue('--text-color');

    const textColorSecondary = documentStyle.getPropertyValue('--text-color-secondary');

    const surfaceBorder = documentStyle.getPropertyValue('--surface-border');

    this.barData.set({
      labels: ['January', 'February', 'March', 'April', 'May', 'June', 'July'],
      datasets: [
        {
          label: 'My First dataset',
          backgroundColor: documentStyle.getPropertyValue('--p-primary-500'),
          borderColor: documentStyle.getPropertyValue('--p-primary-500'),
          data: [65, 59, 80, 81, 56, 55, 40]
        },
        {
          label: 'My Second dataset',
          backgroundColor: documentStyle.getPropertyValue('--p-primary-200'),
          borderColor: documentStyle.getPropertyValue('--p-primary-200'),
          data: [28, 48, 40, 19, 86, 27, 90]
        }
      ]
    });

    this.barOptions.set({
      maintainAspectRatio: false,
      aspectRatio: 0.8,
      plugins: {
        legend: {
          labels: {
            color: textColor
          }
        }
      },
      scales: {
        x: {
          ticks: {
            color: textColorSecondary,
            font: {
              weight: 500
            }
          },
          grid: {
            display: false,
            drawBorder: false
          }
        },
        y: {
          ticks: {
            color: textColorSecondary
          },
          grid: {
            color: surfaceBorder,
            drawBorder: false
          }
        }
      }
    });

    this.pieData.set({
      labels: ['A', 'B', 'C'],
      datasets: [
        {
          data: [540, 325, 702],
          backgroundColor: [documentStyle.getPropertyValue('--p-indigo-500'), documentStyle.getPropertyValue('--p-purple-500'), documentStyle.getPropertyValue('--p-teal-500')],
          hoverBackgroundColor: [documentStyle.getPropertyValue('--p-indigo-400'), documentStyle.getPropertyValue('--p-purple-400'), documentStyle.getPropertyValue('--p-teal-400')]
        }
      ]
    });

    this.pieOptions.set({
      plugins: {
        legend: {
          labels: {
            usePointStyle: true,
            color: textColor
          }
        }
      }
    });

    this.lineData.set({
      labels: ['January', 'February', 'March', 'April', 'May', 'June', 'July'],
      datasets: [
        {
          label: 'First Dataset',
          data: [65, 59, 80, 81, 56, 55, 40],
          fill: false,
          backgroundColor: documentStyle.getPropertyValue('--p-primary-500'),
          borderColor: documentStyle.getPropertyValue('--p-primary-500'),
          tension: 0.4
        },
        {
          label: 'Second Dataset',
          data: [28, 48, 40, 19, 86, 27, 90],
          fill: false,
          backgroundColor: documentStyle.getPropertyValue('--p-primary-200'),
          borderColor: documentStyle.getPropertyValue('--p-primary-200'),
          tension: 0.4
        }
      ]
    });

    this.lineOptions.set({
      maintainAspectRatio: false,
      aspectRatio: 0.8,
      plugins: {
        legend: {
          labels: {
            color: textColor
          }
        }
      },
      scales: {
        x: {
          ticks: {
            color: textColorSecondary
          },
          grid: {
            color: surfaceBorder,
            drawBorder: false
          }
        },
        y: {
          ticks: {
            color: textColorSecondary
          },
          grid: {
            color: surfaceBorder,
            drawBorder: false
          }
        }
      }
    });

    this.polarData.set({
      datasets: [
        {
          data: [11, 16, 7, 3],
          backgroundColor: [documentStyle.getPropertyValue('--p-indigo-500'), documentStyle.getPropertyValue('--p-purple-500'), documentStyle.getPropertyValue('--p-teal-500'), documentStyle.getPropertyValue('--p-orange-500')],
          label: 'My dataset'
        }
      ],
      labels: ['Indigo', 'Purple', 'Teal', 'Orange']
    });

    this.polarOptions.set({
      plugins: {
        legend: {
          labels: {
            color: textColor
          }
        }
      },
      scales: {
        r: {
          grid: {
            color: surfaceBorder
          },
          ticks: {
            display: false,
            color: textColorSecondary
          }
        }
      }
    });

    this.radarData.set({
      labels: ['Eating', 'Drinking', 'Sleeping', 'Designing', 'Coding', 'Cycling', 'Running'],
      datasets: [
        {
          label: 'My First dataset',
          borderColor: documentStyle.getPropertyValue('--p-indigo-400'),
          pointBackgroundColor: documentStyle.getPropertyValue('--p-indigo-400'),
          pointBorderColor: documentStyle.getPropertyValue('--p-indigo-400'),
          pointHoverBackgroundColor: textColor,
          pointHoverBorderColor: documentStyle.getPropertyValue('--p-indigo-400'),
          data: [65, 59, 90, 81, 56, 55, 40]
        },
        {
          label: 'My Second dataset',
          borderColor: documentStyle.getPropertyValue('--p-purple-400'),
          pointBackgroundColor: documentStyle.getPropertyValue('--p-purple-400'),
          pointBorderColor: documentStyle.getPropertyValue('--p-purple-400'),
          pointHoverBackgroundColor: textColor,
          pointHoverBorderColor: documentStyle.getPropertyValue('--p-purple-400'),
          data: [28, 48, 40, 19, 96, 27, 100]
        }
      ]
    });

    this.radarOptions.set({
      plugins: {
        legend: {
          labels: {
            color: textColor
          }
        }
      },
      scales: {
        r: {
          pointLabels: {
            color: textColor
          },
          grid: {
            color: surfaceBorder
          }
        }
      }
    });
  }

  ngOnDestroy() {
    if (this.subscription) {
      this.subscription.unsubscribe();
    }
  }
}
