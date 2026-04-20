import { ensureConfig, getToken } from './api';

type Handler = (event: any) => void;

async function resolveWsUrl(): Promise<string> {
  if (import.meta.env.VITE_WS_URL) return import.meta.env.VITE_WS_URL;
  await ensureConfig();
  try {
    const res = await fetch('/__agency/config');
    if (res.ok) {
      const cfg = await res.json();
      if (cfg.gateway) {
        return cfg.gateway.replace(/^http/, 'ws') + '/ws';
      }
    }
  } catch { /* not in dev mode */ }
  // Default: same-origin WebSocket (works with Vite proxy in dev)
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${location.host}/ws`;
}

class GatewaySocket {
  private ws: WebSocket | null = null;
  private handlers = new Map<string, Set<Handler>>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private _connected = false;
  private _reconnectAttempts = 0;
  private connectionHandlers = new Set<(connected: boolean) => void>();
  private wsUrl: string | null = null;
  private _gaveUp = false;
  private connectStartedAt: number | null = null;

  get reconnectAttempts() {
    return this._reconnectAttempts;
  }

  async connect() {
    if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) return;

    try {
      if (!this.wsUrl) this.wsUrl = await resolveWsUrl();
      this.ws = new WebSocket(this.wsUrl);

      this.ws.onopen = () => {
        this._connected = true;
        this._reconnectAttempts = 0;
        this._gaveUp = false;
        this.connectStartedAt = null;
        this.connectionHandlers.forEach((h) => h(true));
        if (this.reconnectTimer) {
          clearTimeout(this.reconnectTimer);
          this.reconnectTimer = null;
        }
        // Authenticate the WebSocket connection
        const token = getToken();
        if (token) {
          this.ws?.send(JSON.stringify({ type: 'auth', token }));
        }
        // Subscribe to all events — empty arrays = match everything
        this.ws?.send(JSON.stringify({
          type: 'subscribe',
          channels: [],
          agents: [],
          infra: true,
        }));
      };

      this.ws.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          const type = event.type as string;
          this.handlers.get(type)?.forEach((h) => h(event));
          this.handlers.get('*')?.forEach((h) => h(event));
        } catch (err) {
          console.warn('[ws] Failed to parse message:', err);
        }
      };

      this.ws.onclose = () => {
        this.ws = null;
        this._connected = false;
        this._reconnectAttempts++;
        this.connectionHandlers.forEach((h) => h(false));
        if (!this.connectStartedAt) this.connectStartedAt = Date.now();
        if (Date.now() - this.connectStartedAt > 5 * 60 * 1000) {
          this._gaveUp = true;
          this.connectionHandlers.forEach((h) => h(false));
          return;
        }
        const delay = Math.min(500 * Math.pow(2, this._reconnectAttempts - 1), 10000);
        this.reconnectTimer = setTimeout(() => this.connect(), delay);
      };

      this.ws.onerror = (evt) => {
        console.warn('[ws] Connection error:', evt);
        this.ws?.close();
      };
    } catch (err) {
      console.error('[ws] Failed to create WebSocket:', err);
    }
  }

  on(type: string, handler: Handler) {
    if (!this.handlers.has(type)) this.handlers.set(type, new Set());
    this.handlers.get(type)!.add(handler);
    return () => { this.handlers.get(type)?.delete(handler); };
  }

  onConnectionChange(handler: (connected: boolean) => void) {
    this.connectionHandlers.add(handler);
    return () => { this.connectionHandlers.delete(handler); };
  }

  get connected() {
    return this._connected;
  }

  get gaveUp() {
    return this._gaveUp;
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }
}

export const socket = new GatewaySocket();
