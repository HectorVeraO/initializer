import { expect } from '@playwright/test';
import type { APIRequestContext, PlaywrightWorkerArgs } from '@playwright/test';

export interface SeedUser {
  readonly email: string;
  readonly password: string;
}

/** The subset of `data` of GET /api/v1/auth/session the suite reads. */
export interface SessionView {
  readonly account: {
    readonly id: string;
    readonly email: string;
    readonly fullName: string | null;
    readonly role: 'admin' | 'member' | 'viewer' | null;
    readonly isPlatformOperator: boolean;
  };
  readonly activeTenant: {
    readonly id: string;
    readonly slug: string;
    readonly name: string;
    readonly status: string;
  } | null;
  readonly memberships: readonly unknown[];
  readonly flags: Readonly<Record<string, boolean>>;
}

interface KratosFlow {
  readonly ui: {
    readonly action: string;
    readonly nodes: readonly {
      readonly attributes: { readonly name?: string; readonly value?: unknown };
    }[];
  };
}

const FLOW_PATH = '/kratos/self-service/login/browser';
const SESSION_PATH = '/api/v1/auth/session';
const XSRF_COOKIE = 'XSRF-TOKEN';
const XSRF_HEADER = 'X-XSRF-TOKEN';

/** A tenant created by the operator and the token of its first-admin invitation. */
export interface SeedTenant {
  readonly id: string;
  readonly slug: string;
  readonly invitationToken: string;
}

type Role = 'admin' | 'member' | 'viewer';

/**
 * An authenticated API session of one user over its own APIRequestContext.
 * Everything goes through the web base URL (the dev proxy or the deployed
 * origin): no Kratos admin call, no SQL.
 */
export class GatbaseApi {
  private constructor(private readonly request: APIRequestContext) {}

  /**
   * Opens a new context on `baseURL`, signs in through the Kratos browser flow
   * (GET flow, POST password with the csrf token) and checks that the session
   * answers 200. Throws with the status and body otherwise.
   */
  static async signIn(
    playwright: PlaywrightWorkerArgs['playwright'],
    baseURL: string,
    user: SeedUser,
  ): Promise<GatbaseApi> {
    const request = await playwright.request.newContext({ baseURL });
    try {
      const flowResponse = await request.get(FLOW_PATH, {
        headers: { Accept: 'application/json' },
      });
      if (!flowResponse.ok()) {
        throw new Error(
          `login flow: ${flowResponse.status()} ${await flowResponse.text()}`,
        );
      }
      const flow = (await flowResponse.json()) as KratosFlow;
      const csrf = flow.ui.nodes.find(
        (node) => node.attributes.name === 'csrf_token',
      );
      const csrfToken = csrf?.attributes.value;
      if (typeof csrfToken !== 'string') {
        throw new Error('login flow: no csrf_token node');
      }
      const login = await request.post(flow.ui.action, {
        headers: { Accept: 'application/json' },
        data: {
          method: 'password',
          identifier: user.email,
          password: user.password,
          csrf_token: csrfToken,
        },
      });
      if (!login.ok()) {
        throw new Error(`login: ${login.status()} ${await login.text()}`);
      }
      const session = await request.get(SESSION_PATH);
      if (session.status() !== 200) {
        throw new Error(`session: ${session.status()} ${await session.text()}`);
      }
      return new GatbaseApi(request);
    } catch (error) {
      await request.dispose();
      throw error;
    }
  }

  /**
   * Anonymous POST /api/v1/invitations/accept-new on a throwaway context.
   * Returns the created user and the id of the tenant of the membership.
   * Throws unless the answer is 201.
   */
  static async acceptNew(
    playwright: PlaywrightWorkerArgs['playwright'],
    baseURL: string,
    token: string,
    password: string,
    fullName: string,
  ): Promise<SeedUser & { readonly tenantId: string }> {
    const request = await playwright.request.newContext({ baseURL });
    try {
      const response = await request.post('/api/v1/invitations/accept-new', {
        data: { token, password, fullName },
      });
      if (response.status() !== 201) {
        throw new Error(
          `accept-new: ${response.status()} ${await response.text()}`,
        );
      }
      const body = (await response.json()) as {
        data: {
          membership: { tenant: { id: string } };
          account: { email: string };
        };
      };
      return {
        email: body.data.account.email,
        password,
        tenantId: body.data.membership.tenant.id,
      };
    } finally {
      await request.dispose();
    }
  }

  /** Token from an invitationUrl: the `token` param of its `#` fragment. */
  static tokenOf(invitationUrl: string): string {
    const hash = new URL(invitationUrl).hash.slice(1);
    const token = new URLSearchParams(hash).get('token');
    if (token === null || token === '') {
      throw new Error('invitationUrl has no token fragment');
    }
    return token;
  }

  /** POST /api/v1/tenants (operator) -> id, slug and the first-admin invitation token. */
  async createTenant(input: {
    readonly slug: string;
    readonly name: string;
    readonly adminEmail: string;
  }): Promise<SeedTenant> {
    const data = await this.post<{
      tenant: { id: string; slug: string };
      invitationUrl: string;
    }>('/api/v1/tenants', input, 201);
    return {
      id: data.tenant.id,
      slug: data.tenant.slug,
      invitationToken: GatbaseApi.tokenOf(data.invitationUrl),
    };
  }

  /** POST /api/v1/invitations (admin of the active tenant) -> invitation token. */
  async invite(email: string, role: Role): Promise<string> {
    const data = await this.post<{ invitationUrl: string }>(
      '/api/v1/invitations',
      { email, role },
      201,
    );
    return GatbaseApi.tokenOf(data.invitationUrl);
  }

  /** GET /api/v1/auth/session -> data. */
  async session(): Promise<SessionView> {
    const response = await this.request.get(SESSION_PATH);
    expect(response.status()).toBe(200);
    const body = (await response.json()) as { data: SessionView };
    return body.data;
  }

  /** POST with the XSRF header read from the context cookie; throws unless the status is `expected`. Returns `data`. */
  private async post<T>(
    path: string,
    body: unknown,
    expected: number,
  ): Promise<T> {
    const state = await this.request.storageState();
    const cookie = state.cookies.find((entry) => entry.name === XSRF_COOKIE);
    if (cookie === undefined) {
      throw new Error(`no ${XSRF_COOKIE} cookie for POST ${path}`);
    }
    const response = await this.request.post(path, {
      data: body,
      headers: { [XSRF_HEADER]: decodeURIComponent(cookie.value) },
    });
    if (response.status() !== expected) {
      throw new Error(
        `POST ${path}: ${response.status()} ${await response.text()}`,
      );
    }
    const json = (await response.json()) as { data: T };
    return json.data;
  }

  async dispose(): Promise<void> {
    await this.request.dispose();
  }
}
