import { expect, test } from '@playwright/test';
import { expectEnvelope } from '../../tck-support';

test('GET of an unknown API route answers an envelope 404 ROUTE_NOT_FOUND', { tag: '@core' }, async ({ request }) => {
  const response = await request.get('/api/v1/route-that-does-not-exist');
  await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
});

test('DELETE of an unknown API route answers an envelope 404 ROUTE_NOT_FOUND, never plain text', { tag: '@core' }, async ({ request }) => {
  const response = await request.delete('/api/v1/route-that-does-not-exist');
  expect(response.headers()['content-type']).toContain('application/json');
  await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
});

const UNCLEAN_PATHS = ['/api/v1', '/api/v1//route-that-does-not-exist'];

for (const path of UNCLEAN_PATHS) {
  test(`GET ${path} answers an envelope 404 ROUTE_NOT_FOUND, never an HTML redirect`, { tag: '@core' }, async ({ request }) => {
    const response = await request.get(path, { maxRedirects: 0 });
    expect(response.headers()['content-type']).toContain('application/json');
    await expectEnvelope(response, { status: 404, code: 'ROUTE_NOT_FOUND' });
  });
}
