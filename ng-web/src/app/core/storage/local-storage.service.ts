import { Injectable } from '@angular/core';
import { StorageKey } from './storage-keys.const';

@Injectable({ providedIn: 'root' })
export class LocalStorageService {
  getItem(key: StorageKey): string | null {
    return localStorage.getItem(key);
  }

  setItem(key: StorageKey, value: string): void {
    localStorage.setItem(key, value);
  }

  removeItem(key: StorageKey): void {
    localStorage.removeItem(key);
  }
}
