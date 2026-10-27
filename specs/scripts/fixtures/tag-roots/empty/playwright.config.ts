import { defineConfig } from '@playwright/test';

export default defineConfig({ testDir: '.', testMatch: '*/tck/**/*.spec.ts', testIgnore: ['node_modules/**'] });
