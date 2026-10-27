import { HttpBackend, HttpClient } from '@angular/common/http';
import { Injectable, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { RuntimeConfig, RuntimeConfigFailure } from './runtime-config.types';
import { validateRuntimeConfig } from './runtime-config.validation';

const RUNTIME_CONFIG_URL = '/assets/config/runtime-config.json';

/**
 * Loads `runtime-config.json` once, before the first navigation, and shares
 * the result with the HTTP services. `load()` never rejects: a failure is
 * exposed through `failure()` so a guard can send the user to the error page.
 */
@Injectable({ providedIn: 'root' })
export class RuntimeConfigService {
  // Bypasses the interceptors: they need the API URL, which waits on this load.
  private readonly http = new HttpClient(inject(HttpBackend));

  private readonly _config = signal<RuntimeConfig | null>(null);
  private readonly _failure = signal<RuntimeConfigFailure | null>(null);
  private loading: Promise<void> | null = null;

  readonly config = this._config.asReadonly();
  readonly failure = this._failure.asReadonly();
  readonly apiUrl = computed(() => this._config()?.apiUrl ?? '');

  load(): Promise<void> {
    if (this.loading === null) {
      this.loading = this.fetchAndValidate();
    }

    return this.loading;
  }

  /** Resolves with the config, or rejects when it could not be loaded. */
  async whenReady(): Promise<RuntimeConfig> {
    await this.load();

    const config = this._config();

    if (config === null) {
      throw new Error('runtime config unavailable');
    }

    return config;
  }

  /** Absolute-or-same-origin URL of an API path such as `/api/v1/access-profiles`. */
  apiPath(path: string): string {
    return `${this.apiUrl()}${path}`;
  }

  /** True when the URL belongs to the API of this deployment. */
  isApiUrl(url: string): boolean {
    return url.startsWith(`${this.apiUrl()}/api/`);
  }

  private async fetchAndValidate(): Promise<void> {
    let raw: unknown;

    try {
      raw = await firstValueFrom(this.http.get<unknown>(RUNTIME_CONFIG_URL));
    } catch {
      this._failure.set({ kind: 'load' });

      return;
    }

    const result = validateRuntimeConfig(raw);

    if (result.ok) {
      this._config.set(result.config);

      return;
    }

    this._failure.set({ kind: 'invalid', missingFields: result.missingFields });
  }
}
