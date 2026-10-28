import { test, expect } from '@playwright/test';

test('alpha alone', { tag: ['@alpha'] }, async () => {});

test('alpha with beta', { tag: ['@alpha', '@beta'] }, async () => {
  expect(1).toBe(2);
});
