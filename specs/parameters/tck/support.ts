import { strict as assert } from 'node:assert';
import type { APIResponse } from '@playwright/test';
import { expectData, mutate, runSuffix } from '../../tck-support/actors';
import type { Actor } from '../../tck-support/actors';

/** The ParameterView of the contract. */
export interface ParameterView {
  key: string;
  valueType: string;
  effectiveValue: unknown;
  defaultValue: unknown;
  minValue: number | null;
  maxValue: number | null;
  allowedValues: unknown[] | null;
  description: string;
  isSystemRequired: boolean;
  source: 'global' | 'tenant';
}

export interface ParameterDeleted {
  key: string;
  deletedOverrideCount: number;
}

interface ParameterPage {
  items: ParameterView[];
  page: number;
  size: number;
  totalItems: number;
}

/** `tck<runSuffix>.<name>`: a key no other run and no other worker shares. */
export function paramKey(name: string): string {
  return `tck${runSuffix()}.${name}`;
}

/** Asserts the answer is a ParameterView of `key` and returns it. */
export async function expectView(response: APIResponse, status: number, key: string): Promise<ParameterView> {
  const view = await expectData<ParameterView>(response, status);
  assert.equal(view.key, key, 'the answer is not a ParameterView of the requested key');
  return view;
}

/** Operator POST /api/v1/parameters, expecting 201 and a view of the same key. */
export async function defineParameter(operator: Actor, definition: { key: string } & Record<string, unknown>): Promise<ParameterView> {
  return expectView(await mutate(operator, 'POST', '/api/v1/parameters', definition), 201, definition.key);
}

/** Pages GET /api/v1/parameters?size=100 as `actor` until `key` is found. */
export async function readParameter(actor: Actor, key: string): Promise<ParameterView> {
  for (let page = 1; ; page += 1) {
    const result = await expectData<ParameterPage>(await actor.request.get(`/api/v1/parameters?size=100&page=${page}`), 200);
    const found = result.items.find((item) => item.key === key);
    if (found) {
      return found;
    }
    assert.ok(result.items.length > 0 && page * result.size < result.totalItems, `parameter ${key} is not listed`);
  }
}

/** Tenant-override path of a key. */
export function overridePath(key: string): string {
  return `/api/v1/parameters/${key}/tenant-override`;
}

/** The issues of an error as sorted `field:CODE` strings. */
export function issueSet(error: { issues: Array<{ field: string; code: string }> }): string[] {
  return error.issues.map((issue) => `${issue.field}:${issue.code}`).sort();
}

/** A cookie mutation with a raw string body and an explicit Content-Type. */
export async function rawMutate(actor: Actor, method: 'POST' | 'PUT', path: string, body: string, contentType: string): Promise<APIResponse> {
  const state = await actor.request.storageState();
  const csrf = state.cookies.find((cookie) => cookie.name === 'XSRF-TOKEN');
  assert.ok(csrf, 'the actor holds no XSRF-TOKEN cookie');
  return actor.request.fetch(path, { method, headers: { 'X-XSRF-TOKEN': csrf.value, 'Content-Type': contentType }, data: body });
}
