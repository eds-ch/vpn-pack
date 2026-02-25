import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
    formatBytes, relativeTime, isValidCIDR,
    isValidBase64Key, isValidPort, isValidEndpoint,
    isValidMTU, isValidKeepalive, validateTunnelFields,
} from './utils.js';

describe('formatBytes', () => {
    it('returns N/A for null', () => {
        expect(formatBytes(null)).toBe('N/A');
    });

    it('returns N/A for undefined', () => {
        expect(formatBytes(undefined)).toBe('N/A');
    });

    it('formats 0 bytes', () => {
        expect(formatBytes(0)).toBe('0.0 B');
    });

    it('formats bytes below 1024', () => {
        expect(formatBytes(1023)).toBe('1023.0 B');
    });

    it('formats exactly 1024 as KB', () => {
        expect(formatBytes(1024)).toBe('1.0 KB');
    });

    it('formats 1048576 as MB', () => {
        expect(formatBytes(1048576)).toBe('1.0 MB');
    });

    it('formats 1099511627776 as TB', () => {
        expect(formatBytes(1099511627776)).toBe('1.0 TB');
    });

    it('formats fractional values', () => {
        expect(formatBytes(1536)).toBe('1.5 KB');
    });
});

describe('relativeTime', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-02-24T12:00:00Z'));
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('returns never for null', () => {
        expect(relativeTime(null)).toBe('never');
    });

    it('returns never for empty string', () => {
        expect(relativeTime('')).toBe('never');
    });

    it('returns never for Go zero time', () => {
        expect(relativeTime('0001-01-01T00:00:00Z')).toBe('never');
    });

    it('formats 30 seconds ago', () => {
        const date = new Date(Date.now() - 30 * 1000).toISOString();
        expect(relativeTime(date)).toBe('30s ago');
    });

    it('formats 5 minutes ago', () => {
        const date = new Date(Date.now() - 5 * 60 * 1000).toISOString();
        expect(relativeTime(date)).toBe('5m ago');
    });

    it('formats 3 hours ago', () => {
        const date = new Date(Date.now() - 3 * 3600 * 1000).toISOString();
        expect(relativeTime(date)).toBe('3h ago');
    });

    it('formats 2 days ago', () => {
        const date = new Date(Date.now() - 2 * 86400 * 1000).toISOString();
        expect(relativeTime(date)).toBe('2d ago');
    });

    it('returns just now for future timestamps', () => {
        const date = new Date(Date.now() + 60000).toISOString();
        expect(relativeTime(date)).toBe('just now');
    });
});

describe('isValidCIDR', () => {
    it('accepts valid CIDR 10.0.0.0/24', () => {
        expect(isValidCIDR('10.0.0.0/24')).toBe(true);
    });

    it('accepts 0.0.0.0/0', () => {
        expect(isValidCIDR('0.0.0.0/0')).toBe(true);
    });

    it('accepts 255.255.255.255/32', () => {
        expect(isValidCIDR('255.255.255.255/32')).toBe(true);
    });

    it('rejects IP without prefix', () => {
        expect(isValidCIDR('10.0.0.0')).toBe(false);
    });

    it('rejects octet > 255', () => {
        expect(isValidCIDR('256.0.0.0/24')).toBe(false);
    });

    it('rejects prefix > 32', () => {
        expect(isValidCIDR('10.0.0.0/33')).toBe(false);
    });

    it('rejects empty string', () => {
        expect(isValidCIDR('')).toBe(false);
    });

    it('rejects non-CIDR strings', () => {
        expect(isValidCIDR('hello')).toBe(false);
    });
});

describe('isValidBase64Key', () => {
    const validKey = btoa('a]3\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d');

    it('accepts valid 44-char base64 key decoding to 32 bytes', () => {
        expect(isValidBase64Key('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=')).toBe(true);
    });

    it('rejects short string', () => {
        expect(isValidBase64Key('abc')).toBe(false);
    });

    it('rejects empty string', () => {
        expect(isValidBase64Key('')).toBe(false);
    });

    it('rejects null/undefined', () => {
        expect(isValidBase64Key(null)).toBe(false);
        expect(isValidBase64Key(undefined)).toBe(false);
    });

    it('rejects invalid base64 characters', () => {
        expect(isValidBase64Key('!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!='.slice(0, 44))).toBe(false);
    });

    it('rejects 44-char string that decodes to wrong length', () => {
        expect(isValidBase64Key('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/')).toBe(false);
    });
});

describe('isValidPort', () => {
    it('accepts port 1', () => {
        expect(isValidPort(1)).toBe(true);
    });

    it('accepts port 65535', () => {
        expect(isValidPort(65535)).toBe(true);
    });

    it('accepts port as string', () => {
        expect(isValidPort('8080')).toBe(true);
    });

    it('rejects port 0', () => {
        expect(isValidPort(0)).toBe(false);
    });

    it('rejects port 65536', () => {
        expect(isValidPort(65536)).toBe(false);
    });

    it('rejects negative port', () => {
        expect(isValidPort(-1)).toBe(false);
    });

    it('rejects NaN', () => {
        expect(isValidPort('abc')).toBe(false);
    });

    it('rejects float', () => {
        expect(isValidPort(80.5)).toBe(false);
    });
});

describe('isValidEndpoint', () => {
    it('accepts valid IP:port', () => {
        expect(isValidEndpoint('85.12.34.56:51820')).toBe(true);
    });

    it('accepts hostname:port', () => {
        expect(isValidEndpoint('vpn.example.com:51820')).toBe(true);
    });

    it('accepts port 1', () => {
        expect(isValidEndpoint('1.2.3.4:1')).toBe(true);
    });

    it('accepts port 65535', () => {
        expect(isValidEndpoint('1.2.3.4:65535')).toBe(true);
    });

    it('rejects missing port', () => {
        expect(isValidEndpoint('85.12.34.56')).toBe(false);
    });

    it('rejects empty string after colon', () => {
        expect(isValidEndpoint('85.12.34.56:')).toBe(false);
    });

    it('rejects port 0', () => {
        expect(isValidEndpoint('1.2.3.4:0')).toBe(false);
    });

    it('rejects port 65536', () => {
        expect(isValidEndpoint('1.2.3.4:65536')).toBe(false);
    });

    it('rejects just a colon', () => {
        expect(isValidEndpoint(':')).toBe(false);
    });
});

describe('isValidMTU', () => {
    it('accepts 1280', () => {
        expect(isValidMTU(1280)).toBe(true);
    });

    it('accepts 1420 (WireGuard default)', () => {
        expect(isValidMTU(1420)).toBe(true);
    });

    it('accepts 65535', () => {
        expect(isValidMTU(65535)).toBe(true);
    });

    it('rejects 1279', () => {
        expect(isValidMTU(1279)).toBe(false);
    });

    it('rejects 65536', () => {
        expect(isValidMTU(65536)).toBe(false);
    });

    it('rejects NaN', () => {
        expect(isValidMTU('abc')).toBe(false);
    });
});

describe('isValidKeepalive', () => {
    it('accepts 0', () => {
        expect(isValidKeepalive(0)).toBe(true);
    });

    it('accepts 25 (default)', () => {
        expect(isValidKeepalive(25)).toBe(true);
    });

    it('accepts 65535', () => {
        expect(isValidKeepalive(65535)).toBe(true);
    });

    it('rejects -1', () => {
        expect(isValidKeepalive(-1)).toBe(false);
    });

    it('rejects 65536', () => {
        expect(isValidKeepalive(65536)).toBe(false);
    });
});

describe('validateTunnelFields', () => {
    const validData = {
        name: 'office-nyc',
        listenPort: 51820,
        tunnelAddress: '10.255.0.1/30',
        peerPublicKey: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
        peerEndpoint: '85.12.34.56:51820',
        mtu: 1420,
        persistentKeepalive: 25,
        allowedIPs: '10.20.0.0/24',
    };

    it('returns empty object for valid data', () => {
        expect(validateTunnelFields(validData)).toEqual({});
    });

    it('returns errors for empty required fields', () => {
        const errors = validateTunnelFields({
            name: '', listenPort: 0, tunnelAddress: '', peerPublicKey: '',
            mtu: 0, persistentKeepalive: -1,
        });
        expect(errors.name).toBe('Required');
        expect(errors.listenPort).toBeTruthy();
        expect(errors.tunnelAddress).toBe('Required');
        expect(errors.peerPublicKey).toBe('Required');
        expect(errors.mtu).toBeTruthy();
        expect(errors.persistentKeepalive).toBeTruthy();
    });

    it('catches invalid base64 key', () => {
        const errors = validateTunnelFields({ ...validData, peerPublicKey: 'not-a-valid-key' });
        expect(errors.peerPublicKey).toMatch(/44-char base64/);
    });

    it('catches invalid endpoint format', () => {
        const errors = validateTunnelFields({ ...validData, peerEndpoint: 'no-port' });
        expect(errors.peerEndpoint).toBeTruthy();
    });

    it('skips endpoint validation when empty', () => {
        const errors = validateTunnelFields({ ...validData, peerEndpoint: '' });
        expect(errors.peerEndpoint).toBeUndefined();
    });

    it('catches invalid CIDR in allowedIPs string', () => {
        const errors = validateTunnelFields({ ...validData, allowedIPs: '10.0.0.0/24, bad' });
        expect(errors.allowedIPs).toMatch(/Invalid CIDR/);
    });

    it('validates allowedIPs as array', () => {
        const errors = validateTunnelFields({ ...validData, allowedIPs: ['10.0.0.0/24', 'invalid'] });
        expect(errors.allowedIPs).toMatch(/Invalid CIDR/);
    });

    it('passes with valid allowedIPs array', () => {
        const errors = validateTunnelFields({ ...validData, allowedIPs: ['10.0.0.0/24', '192.168.1.0/24'] });
        expect(errors.allowedIPs).toBeUndefined();
    });
});
