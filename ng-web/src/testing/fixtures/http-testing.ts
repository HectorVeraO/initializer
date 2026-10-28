import { HttpTestingController, TestRequest } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

export const CONFIG_URL = '/assets/config/runtime-config.json';

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

/** The one pending request with this method and URL, waiting for it to be issued. */
export function nextCall(method: string, url: string): Promise<TestRequest> {
  const http = TestBed.inject(HttpTestingController);

  return vi.waitFor(() => http.expectOne((request) => request.method === method && request.url === url));
}
