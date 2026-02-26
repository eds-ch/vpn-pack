import { SvelteSet } from 'svelte/reactivity';
import { keepalive } from '../api.js';
import { AUTH_KEEPALIVE_MS } from '../constants.js';

let status = $state({
    backendState: 'Unknown',
    tailscaleIPs: [],
    tailnetName: '',
    authURL: '',
    version: '',
    self: null,
    health: [],
    exitNode: false,
    routes: [],
    peers: [],
    derp: [],
    firewallHealth: null,
    dpiFingerprinting: null,
    integrationStatus: null,
    wgS2sTunnels: [],
    controlURL: '',
    connected: false,
    hostname: '',
    acceptDNS: false,
    acceptRoutes: false,
    shieldsUp: false,
    runSSH: false,
    noSNAT: false,
    udpPort: 0,
    relayServerPort: null,
    relayServerEndpoints: '',
    advertiseTags: [],
});

let errors = $state([]);
let logs = $state([]);
let updateInfo = $state({ available: false, version: '', currentVersion: '', changelogURL: '', dismissed: false });
let changedFields = new SvelteSet();
let nextErrorId = 0;
const ERROR_CAP = 50;
const ERROR_DEDUP_MS = 5000;
let eventSource = null;
let changeTimer = null;
let reconnectTimer = null;
let keepaliveTimer = null;
let sseErrorId = null;
const RECONNECT_DELAY_MS = 3000;

export function getStatus() {
    return status;
}

export function getErrors() {
    return errors;
}

export function getChangedFields() {
    return changedFields;
}

export function getUpdateInfo() {
    return updateInfo;
}

export function dismissUpdate() {
    updateInfo.dismissed = true;
}

export function addError(message) {
    const now = Date.now();
    if (errors.some(e => e.message === message && (now - new Date(e.timestamp).getTime()) < ERROR_DEDUP_MS)) return;
    errors.push({
        id: nextErrorId++,
        message,
        timestamp: new Date(now).toISOString(),
    });
    if (errors.length > ERROR_CAP) errors.splice(0, errors.length - ERROR_CAP);
}

export function dismissError(id) {
    const idx = errors.findIndex(e => e.id === id);
    if (idx !== -1) errors.splice(idx, 1);
}

export function getLogs() {
    return logs;
}

function addLog(level, message) {
    logs.unshift({
        level,
        message,
        timestamp: new Date().toISOString(),
    });
    if (logs.length > 500) logs.length = 500;
}

function valuesEqual(a, b) {
    if (a === b) return true;
    if (a == null || b == null) return a === b;
    if (typeof a !== 'object') return false;
    return JSON.stringify(a) === JSON.stringify(b);
}

export function connect() {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;

    if (eventSource) {
        eventSource.onopen = null;
        eventSource.onerror = null;
        eventSource.onmessage = null;
        eventSource.close();
    }

    eventSource = new EventSource('/vpn-pack/api/events');

    eventSource.onopen = () => {
        status.connected = true;
        if (sseErrorId !== null) {
            dismissError(sseErrorId);
            sseErrorId = null;
        }
    };

    eventSource.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            const changed = new Set();

            for (const key of Object.keys(data)) {
                if (key in status) {
                    if (!valuesEqual(status[key], data[key])) {
                        changed.add(key);
                    }
                }
            }

            if (changed.has('backendState') && data.backendState !== status.backendState) {
                addLog('info', `State changed: ${status.backendState} -> ${data.backendState}`);
            }

            Object.assign(status, data);

            if (changed.size > 0) {
                changedFields.clear();
                for (const key of changed) changedFields.add(key);
                clearTimeout(changeTimer);
                changeTimer = setTimeout(() => {
                    changedFields.clear();
                }, 1000);
            }
        } catch (e) {
            addLog('error', `Failed to parse SSE data: ${e.message}`);
        }
    };

    eventSource.addEventListener('update-available', (event) => {
        try {
            const data = JSON.parse(event.data);
            Object.assign(updateInfo, data);
            updateInfo.dismissed = false;
        } catch (e) {
            addLog('error', `Failed to parse update event: ${e.message}`);
        }
    });

    eventSource.onerror = () => {
        status.connected = false;
        if (sseErrorId === null) {
            sseErrorId = nextErrorId;
            addError('SSE connection lost, reconnecting...');
        }
        if (eventSource.readyState === EventSource.CLOSED && !reconnectTimer) {
            reconnectTimer = setTimeout(() => {
                reconnectTimer = null;
                connect();
            }, RECONNECT_DELAY_MS);
        }
    };

    clearInterval(keepaliveTimer);
    keepaliveTimer = setInterval(keepalive, AUTH_KEEPALIVE_MS);
}

export function disconnect() {
    clearTimeout(changeTimer);
    clearTimeout(reconnectTimer);
    clearInterval(keepaliveTimer);
    reconnectTimer = null;
    keepaliveTimer = null;
    if (eventSource) {
        eventSource.onopen = null;
        eventSource.onerror = null;
        eventSource.onmessage = null;
        eventSource.close();
        eventSource = null;
    }
    status.connected = false;
}
