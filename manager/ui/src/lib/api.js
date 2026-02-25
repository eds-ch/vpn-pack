import { addError } from './stores/tailscale.svelte.js';

const API_BASE = '/vpn-pack/api';

let csrfToken = null;

function extractError(data, status) {
    const err = data?.error;
    if (typeof err === 'string') return err;
    if (err?.message) return err.message;
    return `Request failed: ${status}`;
}

function saveCsrfToken(res) {
    const token = res.headers.get('X-Csrf-Token');
    if (token) csrfToken = token;
}

async function apiFetch(method, path, body) {
    try {
        const headers = {};
        if (csrfToken) headers['X-Csrf-Token'] = csrfToken;
        if (body !== undefined) headers['Content-Type'] = 'application/json';

        const opts = { method, headers };
        if (body !== undefined) opts.body = JSON.stringify(body);

        const res = await fetch(path, opts);
        saveCsrfToken(res);

        if (res.status === 401 || res.status === 403) {
            window.location.href = '/';
            return null;
        }

        const text = await res.text();
        const data = text ? JSON.parse(text) : {};

        if (!res.ok) {
            addError(extractError(data, res.status));
            return null;
        }
        return data;
    } catch (e) {
        addError(`Network error: ${e.message}`);
        return null;
    }
}

export function getStatusOnce() {
    return apiFetch('GET', `${API_BASE}/status`);
}

export async function initCsrf() {
    try {
        const res = await fetch(`${API_BASE}/status`);
        saveCsrfToken(res);
    } catch (_) {}
}

export function tailscaleUp() {
    return apiFetch('POST', `${API_BASE}/tailscale/up`);
}

export function tailscaleDown() {
    return apiFetch('POST', `${API_BASE}/tailscale/down`);
}

export function tailscaleLogout() {
    return apiFetch('POST', `${API_BASE}/tailscale/logout`);
}

export function getDeviceInfo() {
    return apiFetch('GET', `${API_BASE}/device`);
}

export function setRoutes(routes, exitNode) {
    return apiFetch('POST', `${API_BASE}/routes`, { routes, exitNode });
}

export function getSubnets() {
    return apiFetch('GET', `${API_BASE}/subnets`);
}

export function getFirewallStatus() {
    return apiFetch('GET', `${API_BASE}/firewall`);
}

export function connectWithAuthKey(authKey) {
    return apiFetch('POST', `${API_BASE}/tailscale/auth-key`, { authKey });
}

export function getSettings() {
    return apiFetch('GET', `${API_BASE}/settings`);
}

export function setSettings(settings) {
    return apiFetch('POST', `${API_BASE}/settings`, settings);
}

export function getDiagnostics() {
    return apiFetch('GET', `${API_BASE}/diagnostics`);
}

export function fetchLogs() {
    return apiFetch('GET', `${API_BASE}/logs`);
}

// WireGuard S2S
export function wgS2sListTunnels() {
    return apiFetch('GET', `${API_BASE}/wg-s2s/tunnels`);
}
export function wgS2sCreateTunnel(tunnel) {
    return apiFetch('POST', `${API_BASE}/wg-s2s/tunnels`, tunnel);
}
export function wgS2sUpdateTunnel(id, updates) {
    return apiFetch('PATCH', `${API_BASE}/wg-s2s/tunnels/${id}`, updates);
}
export function wgS2sDeleteTunnel(id) {
    return apiFetch('DELETE', `${API_BASE}/wg-s2s/tunnels/${id}`);
}
export function wgS2sEnableTunnel(id) {
    return apiFetch('POST', `${API_BASE}/wg-s2s/tunnels/${id}/enable`);
}
export function wgS2sDisableTunnel(id) {
    return apiFetch('POST', `${API_BASE}/wg-s2s/tunnels/${id}/disable`);
}
export function wgS2sGenerateKeypair() {
    return apiFetch('POST', `${API_BASE}/wg-s2s/generate-keypair`);
}
export function wgS2sGetConfig(id) {
    return apiFetch('GET', `${API_BASE}/wg-s2s/tunnels/${id}/config`);
}
export function wgS2sGetWanIP() {
    return apiFetch('GET', `${API_BASE}/wg-s2s/wan-ip`);
}
export function wgS2sGetLocalSubnets() {
    return apiFetch('GET', `${API_BASE}/wg-s2s/local-subnets`);
}
export function wgS2sListZones() {
    return apiFetch('GET', `${API_BASE}/wg-s2s/zones`);
}

// Integration API
export function getIntegrationStatus() {
    return apiFetch('GET', `${API_BASE}/integration/status`);
}
export function setIntegrationApiKey(apiKey) {
    return apiFetch('POST', `${API_BASE}/integration/api-key`, { apiKey });
}
export function removeIntegrationApiKey() {
    return apiFetch('DELETE', `${API_BASE}/integration/api-key`);
}
export function testIntegrationKey() {
    return apiFetch('POST', `${API_BASE}/integration/test`);
}
