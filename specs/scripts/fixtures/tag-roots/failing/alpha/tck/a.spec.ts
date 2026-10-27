import { test, expect } from '@playwright/test';

test('alpha fails', { tag: '@alpha' }, async () => {
  expect(1).toBe(2);
});
