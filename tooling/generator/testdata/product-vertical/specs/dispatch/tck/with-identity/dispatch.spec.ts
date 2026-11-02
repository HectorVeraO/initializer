import { strict as assert } from 'node:assert';
import { test } from '@playwright/test';
import { bootstrapOperator, expectData, expectError, mutate, runSuffix } from '../../../tck-support/actors';

interface DispatchJob {
  id: string;
  reference: string;
  status: 'queued' | 'running' | 'done';
  createdBy: string | null;
  createdAt: string;
}

interface DispatchJobPage {
  items: DispatchJob[];
  page: number;
  size: number;
  totalItems: number;
}

/** `tck<runSuffix>.<name>`: a reference no other run and no other worker shares. */
function jobReference(name: string): string {
  return `tck${runSuffix()}.${name}`;
}

test('D1 a session queues a job and reads it back', { tag: ['@dispatch', '@identity'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const reference = jobReference('d1');
  const queued = await expectData<DispatchJob>(await mutate(operator, 'POST', '/api/v1/dispatch', { reference }), 201);
  assert.equal(queued.reference, reference);
  assert.equal(queued.status, 'queued');
  assert.equal(queued.createdBy, operator.accountId);

  const page = await expectData<DispatchJobPage>(await operator.request.get('/api/v1/dispatch?size=100'), 200);
  assert.ok(page.items.some((job) => job.id === queued.id), `job ${queued.id} is not listed`);
  await operator.request.dispose();
});

test('D2 a reference already queued answers 409', { tag: ['@dispatch', '@identity'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const reference = jobReference('d2');
  await expectData<DispatchJob>(await mutate(operator, 'POST', '/api/v1/dispatch', { reference }), 201);

  await expectError(await mutate(operator, 'POST', '/api/v1/dispatch', { reference }), 409, 'DISPATCH_REFERENCE_TAKEN');
  await operator.request.dispose();
});

test('D3 a reference that is too long and malformed reports every issue', { tag: ['@dispatch', '@identity'] }, async ({ playwright }) => {
  const operator = await bootstrapOperator(playwright);
  const reference = `${'a'.repeat(61)} bad`;

  const error = await expectError(await mutate(operator, 'POST', '/api/v1/dispatch', { reference }), 422, 'VALIDATION_FAILED');
  assert.deepEqual(
    error.issues.map((issue) => `${issue.field}:${issue.code}`),
    ['reference:FIELD_TOO_LONG', 'reference:FIELD_INVALID_FORMAT'],
  );
  await operator.request.dispose();
});
