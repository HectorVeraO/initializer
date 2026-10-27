import { expect, test } from '@playwright/test';

test('GET /health/live answers 204', { tag: '@core' }, async ({ request }) => {
  const response = await request.get('/health/live');
  expect(response.status()).toBe(204);
});

test('GET /health/ready answers 204 with the database up', { tag: '@core' }, async ({ request }) => {
  const response = await request.get('/health/ready');
  expect(response.status()).toBe(204);
});
