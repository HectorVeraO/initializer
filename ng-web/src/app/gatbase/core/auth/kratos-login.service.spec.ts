import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { configureFeatureTest } from '@/core/testing/ui-testing';
import { CONFIG_URL, fakeLocation, nextCall } from '../../../../testing/fixtures/session-testing';
import { BROWSER_LOCATION } from '../session/browser-location';
import { KratosLoginClient } from './kratos-login.service';
import type { KratosLoginResult } from './kratos.types';

const FLOW_URL = '/kratos/self-service/login/browser';

const ACTION = 'http://localhost/kratos/self-service/login?flow=synthetic-flow';

function flow(action: string = ACTION, nodes: object[] = [csrfNode()]): object {
  return { id: 'synthetic-flow', ui: { action, method: 'POST', nodes, messages: [] } };
}

function csrfNode(value: unknown = 'synthetic-csrf'): object {
  return { attributes: { name: 'csrf_token', type: 'hidden', value }, messages: [] };
}

describe('KratosLoginClient', () => {
  let http: HttpTestingController;

  let isConfigLoaded: boolean;

  beforeEach(() => {
    configureFeatureTest([], [{ provide: BROWSER_LOCATION, useValue: fakeLocation('http://localhost') }]);
    http = TestBed.inject(HttpTestingController);
    isConfigLoaded = false;
  });

  afterEach(() => {
    http.verify();
  });

  /** Starts a sign-in and answers the runtime config request. */
  async function start(): Promise<{ done: Promise<KratosLoginResult> }> {
    const result = TestBed.inject(KratosLoginClient).signIn('admin@example.com', 'synthetic-password');

    // The runtime config is requested by the first sign-in only.
    if (!isConfigLoaded) {
      await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
      isConfigLoaded = true;
    }

    return { done: result };
  }

  async function answerFlow(body: object): Promise<void> {
    const request = await nextCall('GET', FLOW_URL);

    expect(request.request.headers.get('Accept')).toBe('application/json');
    request.flush(body);
  }

  it('posts the password method with the csrf token to the flow action and signs in on 200', async () => {
    const { done: result } = await start();

    await answerFlow(flow());

    const post = await vi.waitFor(() => http.expectOne((request) => request.method === 'POST' && request.url === ACTION));

    expect(post.request.body).toEqual({ method: 'password', identifier: 'admin@example.com', password: 'synthetic-password', csrf_token: 'synthetic-csrf' });
    post.flush({});

    expect(await result).toEqual({ kind: 'signed-in' });
  });

  it('is signed in when Kratos says a session is already available', async () => {
    const { done: result } = await start();

    (await nextCall('GET', FLOW_URL)).flush({ error: { id: 'session_already_available' } }, { status: 400, statusText: 'Bad Request' });

    expect(await result).toEqual({ kind: 'signed-in' });
  });

  it('rejects with the flow and node messages on a 400', async () => {
    const { done: result } = await start();

    await answerFlow(flow());

    const post = await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'));

    const invalid = { id: 4000006, text: 'The provided credentials are invalid.', type: 'error' };

    const nodeMessage = { id: 4000001, text: 'Property password is missing.', type: 'error' };

    post.flush(
      {
        ui: {
          messages: [invalid],
          nodes: [
            { attributes: { name: 'password' }, messages: [nodeMessage] },
            { attributes: { name: 'identifier' }, messages: [] }
          ]
        }
      },
      { status: 400, statusText: 'Bad Request' }
    );

    expect(await result).toEqual({ kind: 'rejected', messages: [invalid, nodeMessage] });
  });

  it('rejects with no messages when the 400 body carries none, dropping malformed entries', async () => {
    const { done: result } = await start();

    await answerFlow(flow());

    const post = await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'));

    post.flush({ ui: { messages: [{ id: 'x', text: 5, type: 'bogus' }, null], nodes: 'nope' } }, { status: 400, statusText: 'Bad Request' });

    expect(await result).toEqual({ kind: 'rejected', messages: [] });
  });

  it.each([
    [410, {}],
    [422, {}],
    [403, { error: { id: 'security_csrf_violation' } }]
  ])('restarts once with a fresh flow after a %i', async (status, body) => {
    const { done: result } = await start();

    await answerFlow(flow());
    (await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'))).flush(body, { status, statusText: 'x' });
    await answerFlow(flow('http://localhost/kratos/self-service/login?flow=second'));

    const second = await vi.waitFor(() => http.expectOne((request) => request.method === 'POST' && request.url.endsWith('flow=second')));

    second.flush({});

    expect(await result).toEqual({ kind: 'signed-in' });
  });

  it('is unavailable after a second 410', async () => {
    const { done: result } = await start();

    await answerFlow(flow());
    (await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'))).flush({}, { status: 410, statusText: 'Gone' });
    await answerFlow(flow());
    (await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'))).flush({}, { status: 410, statusText: 'Gone' });

    expect(await result).toEqual({ kind: 'unavailable' });
  });

  it.each([0, 500, 503])('is unavailable when the post fails with %i', async (status) => {
    const { done: result } = await start();

    await answerFlow(flow());
    (await vi.waitFor(() => http.expectOne((request) => request.method === 'POST'))).flush({}, { status, statusText: 'x' });

    expect(await result).toEqual({ kind: 'unavailable' });
  });

  it('is unavailable when the flow cannot be started', async () => {
    const { done: result } = await start();

    (await nextCall('GET', FLOW_URL)).flush({}, { status: 500, statusText: 'Server Error' });

    expect(await result).toEqual({ kind: 'unavailable' });
  });

  it('is unavailable when the flow carries no csrf token', async () => {
    const { done: result } = await start();

    await answerFlow(flow(ACTION, [csrfNode(42), { attributes: { name: 'identifier' }, messages: [] }]));

    expect(await result).toEqual({ kind: 'unavailable' });
    http.expectNone((request) => request.method === 'POST');
  });

  it.each([
    ['an off-origin host', 'https://evil.example.com/kratos/self-service/login?flow=x'],
    ['another port', 'http://localhost:9999/kratos/self-service/login?flow=x'],
    ['a protocol-relative URL', '//evil.example.com/kratos/self-service/login'],
    ['a javascript: URL', 'javascript:alert(1)'],
    ['a relative path', '/kratos/self-service/login?flow=x'],
    ['a same-origin path outside the login flow', 'http://localhost/kratos/admin/identities'],
    ['a sibling path sharing the prefix', 'http://localhost/kratos/self-service/login-evil'],
    ['a path that climbs out of the login flow', 'http://localhost/kratos/self-service/login/../../admin/identities'],
    ['an encoded climb out of the login flow', 'http://localhost/kratos/self-service/login/%2e%2e/%2e%2e/admin'],
    ['an API path', 'http://localhost/api/v1/auth/logout'],
    ['an empty action', '']
  ])('never posts the password to %s', async (_label, action) => {
    const { done: result } = await start();

    await answerFlow(flow(action));

    expect(await result).toEqual({ kind: 'unavailable' });
    http.expectNone((request) => request.method === 'POST');
  });

  it('only ever posts the password under the login flow path of this origin, whatever the action looks like', async () => {
    // Deterministic generator: mutations of a valid action with characters that change how a URL parses.
    let seed = 20260101;

    const next = (limit: number): number => {
      seed = (seed * 1103515245 + 12345) % 2147483648;

      return seed % limit;
    };

    const pieces = ['/', '..', '%2e%2e', '%2F', '@', ':', '#', '?', String.fromCharCode(92), 'evil.example.com', 'http://', '//', ' ', '\t', '\u0000', 'login', '/kratos', ':8080'];

    for (let round = 0; round < 150; round++) {
      let action = ACTION;

      for (let edit = 0; edit <= next(3); edit++) {
        const at = next(action.length + 1);

        action = action.slice(0, at) + pieces[next(pieces.length)] + action.slice(at + next(4));
      }

      const { done: result } = await start();

      await answerFlow(flow(action));

      const posts = http.match((request) => request.method === 'POST');

      for (const post of posts) {
        const target = new URL(post.request.url);

        expect(target.origin).toBe('http://localhost');
        expect(target.pathname === '/kratos/self-service/login' || target.pathname.startsWith('/kratos/self-service/login/')).toBe(true);
        post.flush({});
      }

      if (posts.length === 0) {
        expect(await result).toEqual({ kind: 'unavailable' });
      } else {
        expect(await result).toEqual({ kind: 'signed-in' });
      }
    }
  });

  it('is unavailable without calling Kratos when the runtime config failed', async () => {
    const result = TestBed.inject(KratosLoginClient).signIn('admin@example.com', 'synthetic-password');

    await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({}, { status: 404, statusText: 'Not Found' }));

    expect(await result).toEqual({ kind: 'unavailable' });
    http.expectNone(FLOW_URL);
  });
});
