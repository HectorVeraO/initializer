import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { BlankComponent, configureFeatureTest } from '@/core/testing/ui-testing';
import { FLAG_SOURCE } from './flag-source.port';
import type { FlagSource } from './flag-source.port';
import { flagGuard } from './flag-guard';

describe('flagGuard', () => {
  async function visit(source: FlagSource | null): Promise<string> {
    configureFeatureTest(
      [
        { path: 'beta-page', canMatch: [flagGuard('beta')], component: BlankComponent },
        { path: 'notfound', component: BlankComponent },
        { path: '**', redirectTo: 'notfound' }
      ],
      source === null ? [] : [{ provide: FLAG_SOURCE, useValue: source }]
    );

    const router = TestBed.inject(Router);

    await router.navigateByUrl('/beta-page');

    return router.url;
  }

  it('makes the path look missing when no flag source is bound (NO_FLAGS)', async () => {
    expect(await visit(null)).toBe('/notfound');
  });

  it('opens the path when the bound source enables the flag', async () => {
    expect(await visit({ isEnabled: (key) => key === 'beta' })).toBe('/beta-page');
  });

  it('keeps the path closed when the bound source disables the flag', async () => {
    expect(await visit({ isEnabled: () => false })).toBe('/notfound');
  });

  it('keeps the path closed for a key the source does not know', async () => {
    const known: Record<string, boolean> = { other: true };

    expect(await visit({ isEnabled: (key) => known[key] === true })).toBe('/notfound');
  });
});
