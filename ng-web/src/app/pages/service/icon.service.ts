import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { map } from 'rxjs/operators';

@Injectable()
export class IconService {
  private http = inject(HttpClient);

  icons!: Record<string, unknown>[];

  selectedIcon: Record<string, unknown> | null = null;

  apiUrl = 'assets/demo/data/icons.json';

  getIcons() {
    return this.http.get<{ icons: Record<string, unknown>[] }>(this.apiUrl).pipe(
      map((response) => {
        this.icons = response.icons;

        return this.icons;
      })
    );
  }
}
