import { HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { errorEnvelope, okEnvelope } from './api-envelope';
import { CONFIG_URL } from './http-testing';
import { sessionView } from './session-view';
import { SessionStore } from '@/platform/core/session/session-store.service';

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

/** A `BROWSER_LOCATION` whose `assign` is a spy; jsdom cannot navigate. */
export function fakeLocation(origin = 'http://localhost'): { origin: string; assign: ReturnType<typeof vi.fn> } {
  return { origin, assign: vi.fn() };
}
