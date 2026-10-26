import { strict as assert } from 'node:assert';
import type { APIRequestContext } from '@playwright/test';
import type { LoginAdapter } from './index';

interface LoginFlow {
  ui: { action: string; nodes: Array<{ attributes: { name?: string; value?: unknown } }> };
}

function isLoginFlow(value: unknown): value is LoginFlow {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const ui = (value as { ui?: unknown }).ui;
  if (typeof ui !== 'object' || ui === null) {
    return false;
  }
  const candidate = ui as { action?: unknown; nodes?: unknown };
  return typeof candidate.action === 'string' && Array.isArray(candidate.nodes);
}

/**
 * Drives the Kratos browser login flow through the public origin's `/kratos/`
 * proxy: create the flow, then submit the password method as JSON.
 */
export const kratosLoginAdapter: LoginAdapter = {
  async login(request: APIRequestContext, email: string, password: string): Promise<void> {
    const created = await request.get('/kratos/self-service/login/browser', { headers: { Accept: 'application/json' } });
    assert.equal(created.status(), 200, 'login: creating the Kratos login flow must answer 200');
    const flow: unknown = await created.json();
    assert.ok(isLoginFlow(flow), 'login: the flow has no ui.action or ui.nodes');

    const csrfNode = flow.ui.nodes.find((node) => node.attributes.name === 'csrf_token');
    const csrfToken = csrfNode?.attributes.value;
    assert.equal(typeof csrfToken, 'string', 'login: the flow has no csrf_token node');

    const submitted = await request.post(flow.ui.action, {
      headers: { Accept: 'application/json' },
      data: { method: 'password', identifier: email, password, csrf_token: csrfToken },
    });
    assert.equal(submitted.status(), 200, 'login: submitting the Kratos login flow must answer 200');
  },
};
