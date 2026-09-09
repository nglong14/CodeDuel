import {
  ApiErrorResponse,
  MatchResponse,
  MatchSnapshot,
  MeResponse,
  SessionResponse,
  User,
} from '../types/api';

const API_BASE = ''; // Same-origin via Vite proxy or direct

class ApiClient {
  private token: string | null = null;
  private onUnauthorized: (() => void) | null = null;

  constructor() {
    this.token = sessionStorage.getItem('codeduel_token');
  }

  setToken(token: string | null) {
    this.token = token;
    if (token) {
      sessionStorage.setItem('codeduel_token', token);
    } else {
      sessionStorage.removeItem('codeduel_token');
    }
  }

  getToken(): string | null {
    return this.token;
  }

  setOnUnauthorized(handler: (() => void) | null) {
    this.onUnauthorized = handler;
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set('Content-Type', 'application/json');

    if (this.token && !headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${this.token}`);
    }

    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers,
    });

    if (!response.ok) {
      let errorData: ApiErrorResponse | null = null;
      try {
        errorData = await response.json();
      } catch {
        // Fallback for non-JSON error
      }
      const message = errorData?.error?.message || `HTTP ${response.status}: ${response.statusText}`;
      const code = errorData?.error?.code || 'unknown_error';
      const error = new Error(message) as Error & { code?: string; status?: number };
      error.code = code;
      error.status = response.status;
      if (response.status === 401) {
        this.onUnauthorized?.();
      }
      throw error;
    }

    return response.json();
  }

  async register(email: string, password: string): Promise<SessionResponse> {
    const data = await this.request<SessionResponse>('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify({ email: email.trim(), password }),
    });
    this.setToken(data.token);
    return data;
  }

  async login(email: string, password: string): Promise<SessionResponse> {
    const data = await this.request<SessionResponse>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email: email.trim(), password }),
    });
    this.setToken(data.token);
    return data;
  }

  async getMe(): Promise<User> {
    const data = await this.request<MeResponse>('/api/me');
    return data.user;
  }

  async getCurrentMatch(): Promise<MatchSnapshot | null> {
    try {
      const data = await this.request<MatchResponse>('/api/me/match');
      return data.match;
    } catch (err: unknown) {
      const apiErr = err as { status?: number; code?: string };
      if (apiErr.status === 404 || apiErr.code === 'match_not_found') {
        return null;
      }
      throw err;
    }
  }

  async getMatchById(matchId: string): Promise<MatchSnapshot> {
    const data = await this.request<MatchResponse>(`/api/matches/${matchId}`);
    return data.match;
  }
}

export const api = new ApiClient();
