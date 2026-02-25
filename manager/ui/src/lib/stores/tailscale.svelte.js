import { SvelteSet } from 'svelte/reactivity';

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
    integrationStatus: null,
    wgS2sTunnels: [],
    controlURL: '',
    connected: false,
});

let errors = $state([]);
let logs = $state([]);
let updateInfo = $state({ available: false, version: '', currentVersion: '', changelogURL: '', dismissed: false });
let changedFields = new SvelteSet();
let nextErrorId = 0;
let eventSource = null;
let changeTimer = null;
let sseErrorId = null;

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
    errors.push({
        id: nextErrorId++,
        message,
        timestamp: new Date().toISOString(),
    });
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

export function connect() {
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
                    const oldVal = JSON.stringify(status[key]);
                    const newVal = JSON.stringify(data[key]);
                    if (oldVal !== newVal) {
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
    };
}

export function disconnect() {
    clearTimeout(changeTimer);
    if (eventSource) {
        eventSource.onopen = null;
        eventSource.onerror = null;
        eventSource.onmessage = null;
        eventSource.close();
        eventSource = null;
    }
    status.connected = false;
}
