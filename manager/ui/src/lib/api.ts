import { addError, addWarning } from './stores/tailscale.svelte.js';
import { AUTH_KEEPALIVE_MS } from './constants.js';
import type {
    Status,
    DeviceInfo,
    OperationResponse,
    SetRoutesResult,
    FirewallStatusResponse,
    SettingsResponse,
    SettingsRequest,
    DiagnosticsResponse,
    LogEntry,
    TunnelInfo,
    TunnelCreateResponse,
    TunnelUpdateResponse,
    EnableTunnelResponse,
    Keypair,
    WgS2sCreateRequest,
    WgS2sZoneEntry,
    IntegrationStatus,
    SubnetEntry,
} from './types.js';

const API_BASE = '/vpn-pack/api';
const DEFAULT_TIMEOUT_MS = 30000;

let csrfToken: string | null = null;
let lastRequestTime = 0;

interface ApiFetchOpts {
    timeout?: number;
    _isRetry?: boolean;
}

function extractError(data: Record<string, unknown>, status: number): string {
    const err = data?.error;
    if (typeof err === 'string') return err;
    if (err && typeof err === 'object' && 'message' in err) return (err as { message: string }).message;
    return `Request failed: ${status}`;
}

async function apiFetch<T>(method: string, path: string, body?: unknown, { timeout = DEFAULT_TIMEOUT_MS, _isRetry = false }: ApiFetchOpts = {}): Promise<T | null> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout);
    try {
        const headers: Record<string, string> = {};
        if (body !== undefined) headers['Content-Type'] = 'application/json';
        if (csrfToken && method !== 'GET') headers['X-Csrf-Token'] = csrfToken;

        const opts: RequestInit = { method, headers, signal: controller.signal };
        if (body !== undefined) opts.body = JSON.stringify(body);

        const res = await fetch(path, opts);
        clearTimeout(timer);

        const newCsrf = res.headers.get('X-Csrf-Token');
        if (newCsrf) csrfToken = newCsrf;

        if (res.status === 401 || res.status === 403) {
            if (!_isRetry) {
                try {
                    const refreshRes = await fetch(`${API_BASE}/status`);
                    const refreshCsrf = refreshRes.headers.get('X-Csrf-Token');
                    if (refreshCsrf) csrfToken = refreshCsrf;
                    if (refreshRes.ok) {
                        return apiFetch<T>(method, path, body, { timeout, _isRetry: true });
                    }
                } catch (e) {
                    console.warn('CSRF refresh failed:', e);
                }
            }
            window.location.href = '/';
            return null;
        }

        lastRequestTime = Date.now();

        const text = await res.text();
        let data: Record<string, unknown> = {};
        if (text) {
            try { data = JSON.parse(text); } catch { /* malformed JSON, treat as empty */ }
        }

        if (!res.ok) {
            const msg = data?.error
                ? extractError(data, res.status)
                : `HTTP ${res.status}: ${text.slice(0, 200)}`;
            addError(msg);
            return null;
        }

        if (data?.setupStatus === 'partial' && (data?.firewall as Record<string, unknown>)?.errors) {
            for (const err of (data.firewall as { errors: string[] }).errors) {
                addWarning(`Firewall: ${err}`);
            }
        }

        return data as T;
    } catch (e: unknown) {
        clearTimeout(timer);
        const error = e as Error;
        if (error.name === 'AbortError') {
            addError(`Request timeout: ${method} ${path}`);
        } else {
            addError(`Network error: ${error.message}`);
        }
        return null;
    }
}

export function getStatusOnce(): Promise<Status | null> {
    return apiFetch<Status>('GET', `${API_BASE}/status`);
}

export function tailscaleUp(): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('POST', `${API_BASE}/tailscale/up`);
}

export function tailscaleDown(): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('POST', `${API_BASE}/tailscale/down`);
}

export function tailscaleLogout(): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('POST', `${API_BASE}/tailscale/logout`);
}

export function getDeviceInfo(): Promise<DeviceInfo | null> {
    return apiFetch<DeviceInfo>('GET', `${API_BASE}/device`);
}

export function setRoutes(routes: string[], exitNode: boolean): Promise<SetRoutesResult | null> {
    return apiFetch<SetRoutesResult>('POST', `${API_BASE}/routes`, { routes, exitNode });
}

export function getSubnets(): Promise<{ subnets: SubnetEntry[] } | null> {
    return apiFetch<{ subnets: SubnetEntry[] }>('GET', `${API_BASE}/subnets`);
}

export function getFirewallStatus(): Promise<FirewallStatusResponse | null> {
    return apiFetch<FirewallStatusResponse>('GET', `${API_BASE}/firewall`);
}

export function connectWithAuthKey(authKey: string): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('POST', `${API_BASE}/tailscale/auth-key`, { authKey });
}

export function getSettings(): Promise<SettingsResponse | null> {
    return apiFetch<SettingsResponse>('GET', `${API_BASE}/settings`);
}

export function setSettings(settings: SettingsRequest): Promise<SettingsResponse | null> {
    return apiFetch<SettingsResponse>('POST', `${API_BASE}/settings`, settings);
}

export function getDiagnostics(): Promise<DiagnosticsResponse | null> {
    return apiFetch<DiagnosticsResponse>('GET', `${API_BASE}/diagnostics`, undefined, { timeout: 60000 });
}

export function fetchLogs(): Promise<{ lines: LogEntry[] } | null> {
    return apiFetch<{ lines: LogEntry[] }>('GET', `${API_BASE}/logs`);
}

// WireGuard S2S
export function wgS2sListTunnels(): Promise<TunnelInfo[] | null> {
    return apiFetch<TunnelInfo[]>('GET', `${API_BASE}/wg-s2s/tunnels`);
}
export function wgS2sCreateTunnel(tunnel: WgS2sCreateRequest): Promise<TunnelCreateResponse | null> {
    return apiFetch<TunnelCreateResponse>('POST', `${API_BASE}/wg-s2s/tunnels`, tunnel);
}
export function wgS2sUpdateTunnel(id: string, updates: Partial<WgS2sCreateRequest>): Promise<TunnelUpdateResponse | null> {
    return apiFetch<TunnelUpdateResponse>('PATCH', `${API_BASE}/wg-s2s/tunnels/${id}`, updates);
}
export function wgS2sDeleteTunnel(id: string): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('DELETE', `${API_BASE}/wg-s2s/tunnels/${id}`);
}
export function wgS2sEnableTunnel(id: string): Promise<EnableTunnelResponse | null> {
    return apiFetch<EnableTunnelResponse>('POST', `${API_BASE}/wg-s2s/tunnels/${id}/enable`);
}
export function wgS2sDisableTunnel(id: string): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('POST', `${API_BASE}/wg-s2s/tunnels/${id}/disable`);
}
export function wgS2sGenerateKeypair(): Promise<Keypair | null> {
    return apiFetch<Keypair>('POST', `${API_BASE}/wg-s2s/generate-keypair`);
}
export function wgS2sGetConfig(id: string): Promise<{ config: string } | null> {
    return apiFetch<{ config: string }>('GET', `${API_BASE}/wg-s2s/tunnels/${id}/config`);
}
export function wgS2sGetWanIP(): Promise<{ ip: string } | null> {
    return apiFetch<{ ip: string }>('GET', `${API_BASE}/wg-s2s/wan-ip`);
}
export function wgS2sGetLocalSubnets(): Promise<{ subnets: SubnetEntry[] } | null> {
    return apiFetch<{ subnets: SubnetEntry[] }>('GET', `${API_BASE}/wg-s2s/local-subnets`);
}
export function wgS2sListZones(): Promise<WgS2sZoneEntry[] | null> {
    return apiFetch<WgS2sZoneEntry[]>('GET', `${API_BASE}/wg-s2s/zones`);
}

// Integration API
export function getIntegrationStatus(): Promise<IntegrationStatus | null> {
    return apiFetch<IntegrationStatus>('GET', `${API_BASE}/integration/status`);
}
export function setIntegrationApiKey(apiKey: string): Promise<IntegrationStatus | null> {
    return apiFetch<IntegrationStatus>('POST', `${API_BASE}/integration/api-key`, { apiKey });
}
export function removeIntegrationApiKey(): Promise<OperationResponse | null> {
    return apiFetch<OperationResponse>('DELETE', `${API_BASE}/integration/api-key`);
}
export function testIntegrationKey(): Promise<IntegrationStatus | null> {
    return apiFetch<IntegrationStatus>('POST', `${API_BASE}/integration/test`);
}

export function keepalive(): Promise<Status | null> | undefined {
    if (Date.now() - lastRequestTime < AUTH_KEEPALIVE_MS * 0.8) return;
    return apiFetch<Status>('GET', `${API_BASE}/status`);
}
