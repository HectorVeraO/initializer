import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import angular from 'angular-eslint';
import prettier from 'eslint-config-prettier';

// Import rules of the Gatbase tree (cookbooks/new-feature.md). Each row of the table in the design is one block below.
const NOT_CORE_MODULE = '^@/gatbase/(?!core(/|$)|modules$)[^/]+';
const NOT_ANY_MODULE_OR_ROOT = '^@/gatbase/(?!core(/|$))';
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
    files: ['src/app/gatbase/core/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: '^@/gatbase/(?!core(/|$))', message: 'The core module never imports another Gatbase module or the composition root.' },
            { regex: SHELL_AND_DEMOS, message: 'The core module never imports the shell, the demo pages or the dev guide.' }
          ]
        }
      ]
    }
  },
  {
    files: ['src/app/gatbase/!(core)/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { regex: '^@/gatbase/core/', message: 'Import the core module through its barrel, @/gatbase/core.' },
            { regex: NOT_ANY_MODULE_OR_ROOT, message: 'A module never imports another Gatbase module or the composition root.' },
            { regex: SHELL_AND_DEMOS, message: 'A module never imports the shell, the demo pages or the dev guide.' },
            { regex: '^(\\.\\./)+(core|parameters|feature-flags)(/|$)', message: 'A module never imports a sibling module folder of the Gatbase tree.' }
          ]
        }
      ]
    }
  },
  {
    files: ['src/app/layout/**/*.ts', 'src/app.*.ts'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [{ regex: `^@/gatbase/core/|${NOT_CORE_MODULE}`, message: 'The skeleton imports Gatbase only through @/gatbase/core and @/gatbase/modules.' }] }]
    }
  },
  {
    files: ['src/app/gatbase/modules.ts'],
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
