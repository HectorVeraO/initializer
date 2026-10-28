import { HttpClient, HttpContext, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { SKIP_ERROR_TOAST } from '@/core/api/api-http-context';
import { RuntimeConfigService } from '@/core/config/runtime-config.service';
import { BROWSER_LOCATION } from '../session/browser-location';
import type { KratosLoginFlow, KratosLoginResult, KratosMessage } from './kratos.types';

// The login view shows its own unavailable text, so the generic error toast stays off.
const KRATOS_CONTEXT = new HttpContext().set(SKIP_ERROR_TOAST, true);

const FLOW_PATH = '/kratos/self-service/login/browser';

const ACTION_PATH = '/kratos/self-service/login';

const MESSAGE_TYPES: readonly string[] = ['error', 'info', 'success'];

type AttemptOutcome = KratosLoginResult | { readonly kind: 'restart' };

/** True when the flow action stays on this origin and under the login flow path: the password is never posted anywhere else. */
function isLoginAction(action: unknown, origin: string): action is string {
  if (typeof action !== 'string') {
    return false;
  }

  let url: URL;

  try {
    url = new URL(action);
  } catch {
    return false;
  }

  const isUnderLoginPath = url.pathname === ACTION_PATH || url.pathname.startsWith(`${ACTION_PATH}/`);

  return url.origin === origin && isUnderLoginPath;
}

function readMessages(value: unknown): KratosMessage[] {
  if (!Array.isArray(value)) {
    return [];
  }

  const messages = value.filter((item): item is KratosMessage => typeof item === 'object' && item !== null && typeof item.id === 'number' && typeof item.text === 'string' && MESSAGE_TYPES.includes(item.type));

  return messages;
}

/** Flow-level messages followed by the messages of each node. */
function collectMessages(body: unknown): KratosMessage[] {
  const ui = (body as { ui?: { messages?: unknown; nodes?: unknown } } | null)?.ui;

  const nodes: unknown[] = Array.isArray(ui?.nodes) ? ui.nodes : [];

  const nodeMessages = nodes.flatMap((node) => readMessages((node as { messages?: unknown } | null)?.messages));

  return [...readMessages(ui?.messages), ...nodeMessages];
}

function csrfTokenOf(flow: KratosLoginFlow): string | null {
  const nodes = Array.isArray(flow.ui?.nodes) ? flow.ui.nodes : [];

  const node = nodes.find((candidate) => candidate.attributes?.name === 'csrf_token');

  const value = node?.attributes.value;

  return typeof value === 'string' ? value : null;
}

function isSessionAlreadyAvailable(error: HttpErrorResponse): boolean {
  const id = (error.error as { error?: { id?: unknown } } | null)?.error?.id;

  return error.status === 400 && id === 'session_already_available';
}

/** Login through the Kratos browser flow in SPA mode, via the same-origin `/kratos/` proxy. Login is the only flow the console implements. */
@Injectable({ providedIn: 'root' })
export class KratosLoginClient {
  private readonly http = inject(HttpClient);
  private readonly runtimeConfig = inject(RuntimeConfigService);
  private readonly browserLocation = inject(BROWSER_LOCATION);

  /** Signs in with email and password. The password is never stored or logged. */
  async signIn(identifier: string, password: string): Promise<KratosLoginResult> {
    await this.runtimeConfig.load();

    if (this.runtimeConfig.failure() !== null) {
      return { kind: 'unavailable' };
    }

    const first = await this.attempt(identifier, password);

    if (first.kind !== 'restart') {
      return first;
    }

    const second = await this.attempt(identifier, password);

    return second.kind === 'restart' ? { kind: 'unavailable' } : second;
  }

  private async attempt(identifier: string, password: string): Promise<AttemptOutcome> {
    let flow: KratosLoginFlow;

    try {
      flow = await firstValueFrom(this.http.get<KratosLoginFlow>(`${this.runtimeConfig.apiUrl()}${FLOW_PATH}`, { headers: { Accept: 'application/json' }, context: KRATOS_CONTEXT }));
    } catch (error) {
      if (error instanceof HttpErrorResponse && isSessionAlreadyAvailable(error)) {
        return { kind: 'signed-in' };
      }

      return { kind: 'unavailable' };
    }

    const action = flow.ui?.action;

    const csrfToken = csrfTokenOf(flow);

    if (!isLoginAction(action, this.browserLocation.origin) || csrfToken === null) {
      return { kind: 'unavailable' };
    }

    try {
      await firstValueFrom(this.http.post(action, { method: 'password', identifier, password, csrf_token: csrfToken }, { headers: { Accept: 'application/json' }, context: KRATOS_CONTEXT }));

      return { kind: 'signed-in' };
    } catch (error) {
      return this.mapFailure(error);
    }
  }

  private mapFailure(error: unknown): AttemptOutcome {
    if (!(error instanceof HttpErrorResponse)) {
      return { kind: 'unavailable' };
    }

    const body = error.error as { error?: { id?: unknown } } | null;

    const isCsrfViolation = error.status === 403 && body?.error?.id === 'security_csrf_violation';

    // A second tab signed in meanwhile: Kratos refuses the POST with the same id as the GET.
    if (isSessionAlreadyAvailable(error)) {
      return { kind: 'signed-in' };
    }

    if (error.status === 400) {
      return { kind: 'rejected', messages: collectMessages(error.error) };
    }

    if (error.status === 410 || error.status === 422 || isCsrfViolation) {
      return { kind: 'restart' };
    }

    return { kind: 'unavailable' };
  }
}
