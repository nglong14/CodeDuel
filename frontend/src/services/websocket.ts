import {
  Envelope,
  SubmitCodeData,
  SupportedLanguage,
} from '../types/proto';
import { generateUUID } from '../utils/uuid';

export type WSConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'error';

export type MessageHandler<T = unknown> = (data: T, raw: Envelope<T>) => void;

class WebSocketClient {
  private ws: WebSocket | null = null;
  private token: string | null = null;
  private status: WSConnectionStatus = 'disconnected';
  private listeners: Map<string, Set<MessageHandler<any>>> = new Map();
  private statusListeners: Set<(status: WSConnectionStatus) => void> = new Set();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempts = 0;
  private shouldReconnect = true;
  private pendingMessages: string[] = [];

  getStatus(): WSConnectionStatus {
    return this.status;
  }

  private setStatus(status: WSConnectionStatus) {
    this.status = status;
    this.statusListeners.forEach((fn) => fn(status));
  }

  onStatusChange(fn: (status: WSConnectionStatus) => void): () => void {
    this.statusListeners.add(fn);
    fn(this.status);
    return () => this.statusListeners.delete(fn);
  }

  connect(token: string) {
    if (this.ws && this.token === token && (this.status === 'connected' || this.status === 'connecting')) {
      return;
    }

    this.disconnect();
    this.token = token;
    this.shouldReconnect = true;
    this.reconnectAttempts = 0;
    this.initSocket();
  }

  private initSocket() {
    if (!this.token) return;

    this.setStatus('connecting');

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const wsUrl = `${protocol}//${host}/ws?token=${encodeURIComponent(this.token)}`;

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        this.reconnectAttempts = 0;
      };

      this.ws.onmessage = (event) => {
        try {
          const envelope: Envelope = JSON.parse(event.data);
          if (envelope.type === 'ready') {
            this.setStatus('connected');
            // Flush any buffered messages that were enqueued while connecting
            while (this.pendingMessages.length > 0 && this.ws?.readyState === WebSocket.OPEN) {
              const msg = this.pendingMessages.shift()!;
              this.ws.send(msg);
            }
          }
          this.dispatch(envelope.type, envelope.data, envelope);
        } catch (err) {
          console.error('[CodeDuel WS] Failed to parse message:', event.data, err);
        }
      };

      this.ws.onerror = (err) => {
        console.warn('[CodeDuel WS] Connection error:', err);
        this.setStatus('error');
      };

      this.ws.onclose = (event) => {
        this.ws = null;
        this.setStatus('disconnected');

        if (event.code === 4401 || event.code === 1008) {
          console.warn('[CodeDuel WS] Server closed with policy/auth error:', event.reason);
          this.shouldReconnect = false;
          this.dispatch('auth_expired', undefined, { type: 'auth_expired' });
          return;
        }

        if (this.shouldReconnect) {
          this.scheduleReconnect();
        }
      };
    } catch (err) {
      console.error('[CodeDuel WS] Failed to create WebSocket:', err);
      this.setStatus('error');
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return;
    const delay = Math.min(1000 * Math.pow(1.5, this.reconnectAttempts), 10000);
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.shouldReconnect && this.token) {
        this.initSocket();
      }
    }, delay);
  }

  disconnect() {
    this.shouldReconnect = false;
    this.pendingMessages = [];
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onerror = null;
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
    this.token = null;
    this.setStatus('disconnected');
  }

  send<T>(type: string, data?: T) {
    if (this.token && !this.shouldReconnect) {
      throw new Error('WebSocket authentication expired');
    }
    const envelope: Envelope<T> = { type, data };
    const serialized = JSON.stringify(envelope);

    if (this.ws && this.ws.readyState === WebSocket.OPEN && this.status === 'connected') {
      this.ws.send(serialized);
    } else {
      // Buffer message until connected
      this.pendingMessages.push(serialized);
      if (this.token && (!this.ws || this.status === 'disconnected')) {
        this.initSocket();
      }
    }
  }

  joinQueue() {
    this.send('join_queue', {});
  }

  leaveQueue() {
    this.send('leave_queue', {});
  }

  submitCode(matchId: string, language: SupportedLanguage, code: string): string {
    const requestId = generateUUID();
    const data: SubmitCodeData = {
      match_id: matchId,
      request_id: requestId,
      language,
      code,
    };
    this.send('submit_code', data);
    return requestId;
  }

  on<T>(type: string, handler: MessageHandler<T>): () => void {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, new Set());
    }
    this.listeners.get(type)!.add(handler);
    return () => {
      this.listeners.get(type)?.delete(handler);
    };
  }

  private dispatch(type: string, data: unknown, raw: Envelope) {
    const handlers = this.listeners.get(type);
    if (handlers) {
      handlers.forEach((fn) => {
        try {
          fn(data, raw);
        } catch (err) {
          console.error(`[CodeDuel WS] Handler error for ${type}:`, err);
        }
      });
    }

    const wildcards = this.listeners.get('*');
    if (wildcards) {
      wildcards.forEach((fn) => {
        try {
          fn(data, raw);
        } catch (err) {
          console.error('[CodeDuel WS] Wildcard handler error:', err);
        }
      });
    }
  }
}

export const wsClient = new WebSocketClient();
