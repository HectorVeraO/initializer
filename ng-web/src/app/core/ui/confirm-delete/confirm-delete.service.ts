import { Injectable, inject, signal } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';
import { Subject, Observable } from 'rxjs';

export interface ConfirmDeleteOptions {
  header?: string;
  message?: string;
  acceptLabel?: string;
}

interface ResolvedOptions {
  header: string;
  message: string;
  acceptLabel: string;
}

interface PendingConfirmation {
  options: ResolvedOptions;
  result$: Subject<boolean>;
}

@Injectable({ providedIn: 'root' })
export class ConfirmDeleteService {
  private readonly transloco = inject(TranslocoService);

  readonly visible = signal(false);
  readonly header = signal('');
  readonly message = signal('');
  readonly confirmText = signal('');
  readonly acceptLabel = signal('');

  private pending: PendingConfirmation | null = null;

  confirm(options: ConfirmDeleteOptions): Observable<boolean> {
    const result$ = new Subject<boolean>();

    const resolved: ResolvedOptions = {
      header: options.header ?? (this.transloco.translate('confirms.confirmDelete') as string),
      message: options.message ?? (this.transloco.translate('confirms.defaultMessage') as string),
      acceptLabel: options.acceptLabel ?? (this.transloco.translate('confirms.delete') as string)
    };

    this.pending = { options: resolved, result$ };
    const normalizedConfirmText = this.extractActionKeyword(resolved.acceptLabel);

    this.header.set(resolved.header);
    this.message.set(resolved.message);
    this.confirmText.set(normalizedConfirmText);
    this.acceptLabel.set(resolved.acceptLabel);
    this.visible.set(true);

    return result$.asObservable();
  }

  accept(): void {
    this.visible.set(false);
    this.pending?.result$.next(true);
    this.pending?.result$.complete();
    this.pending = null;
  }

  reject(): void {
    this.visible.set(false);
    this.pending?.result$.next(false);
    this.pending?.result$.complete();
    this.pending = null;
  }

  private extractActionKeyword(label: string): string {
    const match = label.match(/[A-Za-zÁÉÍÓÚÜÑáéíóúüñ]+/);

    const keyword = match?.[0] ?? label;

    return keyword.trim().toLocaleLowerCase('es-CL');
  }
}
