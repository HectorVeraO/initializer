import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import angular from 'angular-eslint';
import prettier from 'eslint-config-prettier';
import { readdirSync } from 'node:fs';

// Module folders of the platform tree, read once at load time: every directory under src/app/platform except core.
const moduleFolders = readdirSync(new URL('./src/app/platform/', import.meta.url), { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && entry.name !== 'core')
  .map((entry) => entry.name);
const escapeRegex = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
// The composition root (modules.ts) is a file, so it joins the folder names by hand.
const optionalNames = [...moduleFolders, 'modules'].map(escapeRegex).join('|');
const RELATIVE_TO_OPTIONAL = `^(\\.\\./)+(platform/)?(${optionalNames})(/|$)`;
const RELATIVE_TO_ANY_SIBLING = `^(\\.\\./)+(platform/)?(core|${optionalNames})(/|$)`;

// Import rules of the platform tree (cookbooks/new-feature.md). Each row of the table in the design is one block below.
// Alias or relative specifiers into a module folder of the platform tree (everything but core and the composition root).
const ANY_MODULE_FOLDER = '(^|/)platform/(?!core(/|$)|modules$)';
// The same, plus a deep import into core: the skeleton goes through the core barrel.
const ANY_MODULE_FOLDER_OR_CORE_DEEP = '(^|/)platform/(?!core$|modules$)';
const NOT_ANY_MODULE_OR_ROOT = '^@/platform/(?!core(/|$))';
const SHELL_AND_DEMOS = '^@/(layout|pages|dev)(/|$)';

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
      'no-restricted-imports': ['error', { patterns: [{ regex: ANY_MODULE_FOLDER, message: 'Core-owned code imports a module only through @/platform/modules, never its folder.' }] }]
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
    files: ['src/app/platform/modules.ts'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [{ regex: '^(@/|\\.\\./)', message: 'The composition root imports only ./core and ./<module>/module.' }] }]
    }
  },
  {
    files: ['**/*.html'],
    extends: [...angular.configs.templateRecommended, ...angular.configs.templateAccessibility, prettier],
    rules: {}
  }
);
