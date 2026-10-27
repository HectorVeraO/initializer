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

  /** GET /api/v1/auth/session -> data. */
  async session(): Promise<SessionView> {
    const response = await this.request.get(SESSION_PATH);
    expect(response.status()).toBe(200);
    const body = (await response.json()) as { data: SessionView };
    return body.data;
  }

  async dispose(): Promise<void> {
    await this.request.dispose();
  }
}
