import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import angular from 'angular-eslint';
import prettier from 'eslint-config-prettier';
import { existsSync, readdirSync } from 'node:fs';

// Module folders of the platform tree, read once at load time: every directory under src/app/platform except core.
const moduleFolders = readdirSync(new URL('./src/app/platform/', import.meta.url), { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && entry.name !== 'core')
  .map((entry) => entry.name);
const escapeRegex = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
// The composition root (modules.ts) is a file, so it joins the folder names by hand.
const optionalNames = [...moduleFolders, 'modules'].map(escapeRegex).join('|');
const RELATIVE_TO_OPTIONAL = `^(\\.\\./)+(platform/)?(${optionalNames})(/|$)`;
const RELATIVE_TO_ANY_SIBLING = `^(\\.\\./)+(platform/)?(core|${optionalNames})(/|$)`;

// Product verticals of the project, read once at load time like the platform folders. A generated
// project with no web vertical ships no src/app/product/ folder, so the read is guarded.
const productRoot = new URL('./src/app/product/', import.meta.url);
const productFolders = existsSync(productRoot)
  ? readdirSync(productRoot, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name)
  : [];

// Import rules of the platform tree and of the product verticals (cookbooks/new-feature.md). Each row of the table in the design is one block below.
// Alias or relative specifiers into a module folder of the platform tree (everything but core and the composition root).
const ANY_MODULE_FOLDER = '(^|/)platform/(?!core(/|$)|modules$)';
// The same, plus a deep import into core: the skeleton goes through the core barrel.
const ANY_MODULE_FOLDER_OR_CORE_DEEP = '(^|/)platform/(?!core$|modules$)';
const NOT_ANY_MODULE_OR_ROOT = '^@/platform/(?!core(/|$))';
const SHELL_AND_DEMOS = '^@/(layout|pages|dev)(/|$)';
// A product vertical, or the project's own registry seed, by alias or by a relative path that leaves
// the importing folder.
const PRODUCT_ALIAS = '^@/product(\\.modules)?(/|$)';
const RELATIVE_TO_PRODUCT = `^(\\.\\./)+product(\\.modules)?(/|$)`;
// Every upward import is an escape, except the shared test fixtures, which no alias reaches.
const RELATIVE_ESCAPE = '^\\.\\./(?!(\\.\\./)*testing/fixtures/)';
// What a product vertical may reach by alias: the platform core barrel and the composition root.
const PRODUCT_ALIAS_LIMIT = '^@/(?!platform/(core|modules)$)';
const PRODUCT_IMPORT = 'A product vertical imports the platform through @/platform/core and @/platform/modules, and its own files through ./ or @/product/<vertical>.';
const PRODUCT_CALLER = 'Platform code never imports a product vertical: the composition root installs it.';

export default tseslint.config(
  { ignores: ['.angular/**', 'node_modules/**', 'dist/**'] },
  {
    files: ['**/*.ts'],
    extends: [eslint.configs.recommended, ...tseslint.configs.recommended, ...tseslint.configs.stylistic, ...angular.configs.tsRecommended, prettier],
    processor: angular.processInlineTemplates,
    rules: {
      '@angular-eslint/component-selector': ['error', { type: 'element', prefix: ['app', 'p'], style: 'kebab-case' }],
      '@angular-eslint/directive-selector': ['error', { type: 'attribute', prefix: ['app', 'p'], style: 'camelCase' }],
      // Components declare ChangeDetectionStrategy.Eager explicitly. State written from async callbacks
      // is held in signals, but components may still mutate plain fields and input objects in place,
      // which OnPush would stop rendering.
      '@angular-eslint/prefer-on-push-component-change-detection': 'off',
      'padding-line-between-statements': ['error', { blankLine: 'always', prev: ['const', 'let', 'var'], next: '*' }, { blankLine: 'always', prev: '*', next: 'return' }]
    }
  },
  {
    files: ['src/**/*.ts'],
    ignores: ['src/app/platform/**'],
    rules: {
      // A deep import into platform core is a module-path coupling too, so the plain src tree is held to
      // the barrel as well.
      'no-restricted-imports': ['error', { patterns: [{ regex: ANY_MODULE_FOLDER_OR_CORE_DEEP, message: 'Core-owned code imports a module only through @/platform/modules, never its folder.' }] }]
    }
  },
  {
    files: ['src/app/platform/core/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: '^@/platform/(?!core(/|$))', message: 'The core module never imports another platform module or the composition root.' },
            { regex: RELATIVE_TO_OPTIONAL, message: 'The core module never imports another platform module or the composition root.' },
            { regex: PRODUCT_ALIAS, message: PRODUCT_CALLER },
            { regex: RELATIVE_TO_PRODUCT, message: PRODUCT_CALLER },
            { regex: SHELL_AND_DEMOS, message: 'The core module never imports the shell, the demo pages or the dev guide.' }
          ]
        }
      ]
    }
  },
  {
    files: ['src/app/platform/!(core)/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: '^@/platform/core/', message: 'Import the core module through its barrel, @/platform/core.' },
            { regex: NOT_ANY_MODULE_OR_ROOT, message: 'A module never imports another platform module or the composition root.' },
            { regex: PRODUCT_ALIAS, message: PRODUCT_CALLER },
            { regex: RELATIVE_TO_PRODUCT, message: PRODUCT_CALLER },
            { regex: SHELL_AND_DEMOS, message: 'A module never imports the shell, the demo pages or the dev guide.' },
            { regex: RELATIVE_TO_ANY_SIBLING, message: 'A module never imports a sibling module folder of the platform tree.' }
          ]
        }
      ]
    }
  },
  {
    files: ['src/app/layout/**/*.ts', 'src/app.*.ts'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [{ regex: ANY_MODULE_FOLDER_OR_CORE_DEEP, message: 'The skeleton imports platform modules only through @/platform/core and @/platform/modules.' }] }]
    }
  },
  {
    // A product vertical reaches the platform only through the two barrels, and its own files relatively.
    // A vertical moved or renamed later keeps this block on the next run, because the folders are read at load time.
    files: ['src/app/product/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: PRODUCT_ALIAS_LIMIT, message: PRODUCT_IMPORT },
            { regex: RELATIVE_ESCAPE, message: PRODUCT_IMPORT }
          ]
        }
      ]
    }
  },
  // One block per vertical, after the generic product block: flat config replaces no-restricted-imports
  // wholesale, so this is the only place the vertical's own folder joins the allowed aliases.
  ...productFolders.map((folder) => ({
    files: [`src/app/product/${folder}/**/*.ts`],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: `^@/(?!platform/(core|modules)$|product/${escapeRegex(folder)}(/|$))`, message: PRODUCT_IMPORT },
            { regex: RELATIVE_ESCAPE, message: PRODUCT_IMPORT }
          ]
        }
      ]
    }
  })),
  {
    files: ['src/app/platform/modules.ts'],
    rules: {
      // Last, because a later block replaces no-restricted-imports wholesale: the composition root imports
      // ./core, ./<module>/module and the project's ../product.modules, nothing else.
      'no-restricted-imports': ['error', { patterns: [{ regex: '^(@/|\\.\\./(?!product\\.modules$))', message: 'The composition root imports only ./core, ./<module>/module and ../product.modules.' }] }]
    }
  },
  {
    files: ['**/*.html'],
    extends: [...angular.configs.templateRecommended, ...angular.configs.templateAccessibility, prettier],
    rules: {}
  }
);
