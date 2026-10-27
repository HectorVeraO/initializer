import { Injectable } from '@angular/core';
import { StorageKey } from './storage-keys.const';

@Injectable({ providedIn: 'root' })
export class SessionStorageService {
  getItem(key: StorageKey): string | null {
    return sessionStorage.getItem(key);
  }

  setItem(key: StorageKey, value: string): void {
    sessionStorage.setItem(key, value);
  }

  removeItem(key: StorageKey): void {
    sessionStorage.removeItem(key);
  }

  getJson<T>(key: StorageKey): T | null {
    const raw = this.getItem(key);

    if (raw == null) return null;
    try {
      return JSON.parse(raw) as T;
    } catch {
      return null;
    }
  }

  setJson<T>(key: StorageKey, value: T): void {
    this.setItem(key, JSON.stringify(value));
  }
}
