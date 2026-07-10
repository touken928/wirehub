import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { StatusSocket } from '@/ws/status';

class FakeWebSocket {
  static current: FakeWebSocket | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(_url: string) {
    FakeWebSocket.current = this;
  }

  close() {}

  emitClose(code: number) {
    this.onclose?.({ code } as CloseEvent);
  }
}

describe('status socket auth handling', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { location: { protocol: 'http:', host: 'localhost' } });
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    FakeWebSocket.current = null;
  });

  it('validates a 1006 close and stops for an expired session', async () => {
    const unauthorized = vi.fn();
    vi.mocked(fetch).mockResolvedValue({ status: 401 } as Response);
    const socket = new StatusSocket({ onUnauthorized: unauthorized });

    socket.connect('expired-token');
    FakeWebSocket.current?.emitClose(1006);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(fetch).toHaveBeenCalledWith('/api/settings', {
      headers: { Authorization: 'Bearer expired-token' },
    });
    expect(unauthorized).toHaveBeenCalledOnce();
    socket.close();
  });
});
