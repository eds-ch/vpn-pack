import {
    BYTES_PER_KB, SECONDS_PER_MINUTE, SECONDS_PER_HOUR, SECONDS_PER_DAY,
    OCTET_MAX, CIDR_PREFIX_MAX, BASE64_KEY_LENGTH, DECODED_KEY_BYTES,
    PORT_MIN, PORT_MAX, MTU_MIN, KEEPALIVE_MIN,
} from './constants.js';

export function formatBytes(bytes) {
    if (bytes == null) return 'N/A';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let val = bytes;
    while (val >= BYTES_PER_KB && i < units.length - 1) {
        val /= BYTES_PER_KB;
        i++;
    }
    return `${val.toFixed(1)} ${units[i]}`;
}

export function relativeTime(dateStr) {
    if (!dateStr || dateStr === '0001-01-01T00:00:00Z') return 'never';
    const diff = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
    if (diff < 0) return 'just now';
    if (diff < SECONDS_PER_MINUTE) return `${diff}s ago`;
    if (diff < SECONDS_PER_HOUR) return `${Math.floor(diff / SECONDS_PER_MINUTE)}m ago`;
    if (diff < SECONDS_PER_DAY) return `${Math.floor(diff / SECONDS_PER_HOUR)}h ago`;
    return `${Math.floor(diff / SECONDS_PER_DAY)}d ago`;
}

export function isValidCIDR(value) {
    const match = value.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/);
    if (!match) return false;
    const octets = [match[1], match[2], match[3], match[4]].map(Number);
    const prefix = Number(match[5]);
    return octets.every(o => o >= 0 && o <= OCTET_MAX) && prefix >= 0 && prefix <= CIDR_PREFIX_MAX;
}

export function isValidBase64Key(value) {
    if (!value || value.length !== BASE64_KEY_LENGTH) return false;
    try {
        return atob(value).length === DECODED_KEY_BYTES;
    } catch {
        return false;
    }
}

export function isValidPort(value) {
    const n = Number(value);
    return Number.isInteger(n) && n >= PORT_MIN && n <= PORT_MAX;
}

export function isValidEndpoint(value) {
    const lastColon = value.lastIndexOf(':');
    if (lastColon <= 0) return false;
    const port = value.slice(lastColon + 1);
    if (!port) return false;
    const portNum = Number(port);
    return Number.isInteger(portNum) && portNum >= PORT_MIN && portNum <= PORT_MAX;
}

export function isValidMTU(value) {
    const n = Number(value);
    return Number.isInteger(n) && n >= MTU_MIN && n <= PORT_MAX;
}

export function isValidKeepalive(value) {
    const n = Number(value);
    return Number.isInteger(n) && n >= KEEPALIVE_MIN && n <= PORT_MAX;
}

export function validateTunnelFields(data) {
    const errors = {};
    if (!data.name?.trim()) errors.name = 'Required';
    if (!isValidPort(data.listenPort)) errors.listenPort = `Must be ${PORT_MIN}-${PORT_MAX}`;
    if (!data.tunnelAddress?.trim()) errors.tunnelAddress = 'Required';
    else if (!isValidCIDR(data.tunnelAddress.trim())) errors.tunnelAddress = 'Invalid CIDR (e.g. 10.255.0.1/30)';
    if (!data.peerPublicKey?.trim()) errors.peerPublicKey = 'Required';
    else if (!isValidBase64Key(data.peerPublicKey.trim())) errors.peerPublicKey = `Invalid key (must be ${BASE64_KEY_LENGTH}-char base64)`;
    if (data.peerEndpoint?.trim() && !isValidEndpoint(data.peerEndpoint.trim())) {
        errors.peerEndpoint = 'Invalid format (e.g. 85.12.34.56:51820)';
    }
    if (!isValidMTU(data.mtu)) errors.mtu = `Must be ${MTU_MIN}-${PORT_MAX}`;
    if (!isValidKeepalive(data.persistentKeepalive)) errors.persistentKeepalive = `Must be ${KEEPALIVE_MIN}-${PORT_MAX}`;

    const ips = typeof data.allowedIPs === 'string'
        ? data.allowedIPs.split(',').map(s => s.trim()).filter(Boolean)
        : Array.isArray(data.allowedIPs) ? data.allowedIPs : [];
    for (const c of ips) {
        if (!isValidCIDR(c)) {
            errors.allowedIPs = `Invalid CIDR: ${c}`;
            break;
        }
    }
    return errors;
}

export const stateColors = {
    Running: 'bg-success',
    NeedsLogin: 'bg-warning',
    Stopped: 'bg-error',
    Starting: 'bg-blue',
};

export const stateLabels = {
    Running: 'Running',
    NeedsLogin: 'Needs Login',
    Stopped: 'Stopped',
    Starting: 'Starting',
    Unknown: 'Unknown',
    Unavailable: 'Unavailable',
};

export function sortPeers(peers) {
    return [...peers].sort((a, b) => {
        if (a.online !== b.online) return a.online ? -1 : 1;
        return (a.hostName || '').localeCompare(b.hostName || '');
    });
}

export function tunnelStatusInfo(tunnel) {
    if (tunnel.connected) return { dot: 'bg-success', label: 'Connected' };
    if (tunnel.lastHandshake && tunnel.lastHandshake !== '0001-01-01T00:00:00Z') {
        return { dot: 'bg-warning', label: 'Handshake stale' };
    }
    return { dot: 'bg-text-secondary', label: 'No handshake' };
}
