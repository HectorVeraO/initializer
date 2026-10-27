import { HttpTestingController, TestRequest } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { errorEnvelope } from './api-envelope';
import { okEnvelope, sessionView } from './session-view';
import { SessionStore } from '@/gatbase/core/session/session-store.service';

export const CONFIG_URL = '/assets/config/runtime-config.json';

export const SESSION_URL = '/api/v1/auth/session';

/** Loads the session store through its first load: `view` answers 200, `null` answers 401. */
export async function loadSession(view: object | null = sessionView()): Promise<void> {
  const http = TestBed.inject(HttpTestingController);

  const loaded = TestBed.inject(SessionStore).ensureLoaded();

  await vi.waitFor(() => http.expectOne(CONFIG_URL).flush({ apiUrl: '' }));
  await vi.waitFor(() => {
    const request = http.expectOne(SESSION_URL);

    if (view === null) {
      request.flush(errorEnvelope('SESSION_INVALID'), { status: 401, statusText: 'Unauthorized' });
    } else {
      request.flush(okEnvelope(view));
    }
  });
  await loaded;
}

/** Stand-in for `BroadcastChannel`: records what a tab posts and lets a spec deliver a message from "another tab". */
export class FakeBroadcastChannel {
  static instances: FakeBroadcastChannel[] = [];

  onmessage: ((event: { data: unknown }) => void) | null = null;
  readonly posted: unknown[] = [];
  closed = false;

  constructor(readonly name: string) {
    FakeBroadcastChannel.instances.push(this);
  }

  postMessage(message: unknown): void {
    this.posted.push(message);
  }

  close(): void {
    this.closed = true;
  }

  deliver(data: unknown): void {
    this.onmessage?.({ data });
  }
}

/** Replaces the global `BroadcastChannel`; call before the session store is first injected and `vi.unstubAllGlobals()` afterwards. */
export function stubBroadcastChannel(): void {
  FakeBroadcastChannel.instances = [];
  vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel);
}

/** A `BROWSER_LOCATION` whose `assign` is a spy; jsdom cannot navigate. */
export function fakeLocation(origin = 'http://localhost'): { origin: string; assign: ReturnType<typeof vi.fn> } {
  return { origin, assign: vi.fn() };
}

/** The one pending request with this method and URL, waiting for it to be issued. */
export function nextCall(method: string, url: string): Promise<TestRequest> {
  const http = TestBed.inject(HttpTestingController);

  return vi.waitFor(() => http.expectOne((request) => request.method === method && request.url === url));
}
