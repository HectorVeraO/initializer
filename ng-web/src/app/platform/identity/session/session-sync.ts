import type { SessionState } from './session-state';

/** Message sent to the other tabs when this tab changed the session or its active tenant. */
export interface SessionChangedMessage {
  readonly type: 'session-changed';
  readonly tenantId: string | null;
  readonly state: SessionState;
}

const CHANNEL_NAME = 'platform-session';

const STATES: readonly string[] = ['unknown', 'anonymous', 'authenticated'];

function isSessionChanged(data: unknown): data is SessionChangedMessage {
  if (typeof data !== 'object' || data === null) {
    return false;
  }

  const record = data as Record<string, unknown>;

  const hasValidTenant = record['tenantId'] === null || typeof record['tenantId'] === 'string';

  const hasValidState = typeof record['state'] === 'string' && STATES.includes(record['state']);

  return record['type'] === 'session-changed' && hasValidTenant && hasValidState;
}

/** Cross-tab channel of the session. Without `BroadcastChannel` it does nothing. */
export class SessionSync {
  private readonly channel: BroadcastChannel | null;

  constructor(onChanged: (message: SessionChangedMessage) => void) {
    this.channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL_NAME);

    if (this.channel !== null) {
      this.channel.onmessage = (event: MessageEvent<unknown>) => {
        if (isSessionChanged(event.data)) {
          onChanged(event.data);
        }
      };
    }
  }

  post(message: SessionChangedMessage): void {
    this.channel?.postMessage(message);
  }

  close(): void {
    this.channel?.close();
  }
}
