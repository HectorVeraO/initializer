import { Injectable } from '@angular/core';

/** Holds one invitation token in memory across the in-app login; never persisted. */
@Injectable({ providedIn: 'root' })
export class PendingInvitation {
  private token: string | null = null;

  set(token: string): void {
    this.token = token;
  }

  /** Returns the token and forgets it. */
  take(): string | null {
    const token = this.token;

    this.token = null;

    return token;
  }
}
