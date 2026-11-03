# Brand files

The console ships with a typographic wordmark and a typographic sign-in hero, so
a project needs no artwork to look complete. To use your own files, put them in
this folder (for example `logo.svg` and `hero.jpg`) and point the two slots in
`src/app/core/config/brand.ts` at them:

- `BRAND_LOGO_URL = '/brand/logo.svg'` replaces the topbar wordmark with an image.
- `BRAND_HERO_URL = '/brand/hero.jpg'` replaces the typographic panel of the sign-in
  screen with an image.

Files under this folder are served from the site root: `/brand/<file>`. Keep each
file small: it is copied into every generated project and loaded on first paint.
