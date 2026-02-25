import { vi, describe, it, expect, beforeEach } from 'vitest';

vi.mock('./stores/tailscale.svelte.js', () => ({
    addError: vi.fn(),
}));

import { addError } from './stores/tailscale.svelte.js';
import {
    getStatusOnce,
    setSettings,
    tailscaleUp,
    wgS2sDeleteTunnel,
    connectWithAuthKey,
    getDeviceInfo,
    initCsrf,
} from './api.js';

function mockFetch(overrides = {}) {
    const defaults = {
        ok: true,
        status: 200,
        text: () => Promise.resolve(JSON.stringify({ data: 'test' })),
        headers: new Headers(),
    };
    const res = { ...defaults, ...overrides };
    return vi.fn().mockResolvedValue(res);
}

describe('api', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        global.fetch = mockFetch();
    });

    it('calls correct URL for GET request', async () => {
        const result = await getStatusOnce();
        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/status',
            expect.objectContaining({ method: 'GET' }),
        );
        expect(result).toEqual({ data: 'test' });
    });

    it('extracts and sends CSRF token', async () => {
        const headers = new Headers();
        headers.set('X-Csrf-Token', 'my-csrf-token');
        global.fetch = mockFetch({ headers });

        await getStatusOnce();

        global.fetch = mockFetch();
        await getDeviceInfo();

        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/device',
            expect.objectContaining({
                headers: expect.objectContaining({
                    'X-Csrf-Token': 'my-csrf-token',
                }),
            }),
        );
    });

    it('sends POST with JSON body for setSettings', async () => {
        await setSettings({ hostname: 'test' });

        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/settings',
            expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ hostname: 'test' }),
            }),
        );
    });

    it('returns null on 401 response', async () => {
        global.fetch = mockFetch({
            ok: false,
            status: 401,
            text: () => Promise.resolve(''),
        });

        const result = await getStatusOnce();
        expect(result).toBeNull();
    });

    it('calls addError on 500 response with error string', async () => {
        global.fetch = mockFetch({
            ok: false,
            status: 500,
            text: () => Promise.resolve(JSON.stringify({ error: 'boom' })),
        });

        const result = await getStatusOnce();
        expect(result).toBeNull();
        expect(addError).toHaveBeenCalledWith('boom');
    });

    it('calls addError on 500 response with error object', async () => {
        global.fetch = mockFetch({
            ok: false,
            status: 500,
            text: () => Promise.resolve(JSON.stringify({ error: { message: 'detailed error' } })),
        });

        await getStatusOnce();
        expect(addError).toHaveBeenCalledWith('detailed error');
    });

    it('calls addError on network error', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

        const result = await getStatusOnce();
        expect(result).toBeNull();
        expect(addError).toHaveBeenCalledWith('Network error: Failed to fetch');
    });

    it('tailscaleUp calls correct endpoint', async () => {
        await tailscaleUp();
        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/tailscale/up',
            expect.objectContaining({ method: 'POST' }),
        );
    });

    it('wgS2sDeleteTunnel calls correct endpoint with id', async () => {
        await wgS2sDeleteTunnel('tunnel-123');
        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/wg-s2s/tunnels/tunnel-123',
            expect.objectContaining({ method: 'DELETE' }),
        );
    });

    it('connectWithAuthKey sends auth key in body', async () => {
        await connectWithAuthKey('tskey-auth-abc123');
        expect(global.fetch).toHaveBeenCalledWith(
            '/vpn-pack/api/tailscale/auth-key',
            expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ authKey: 'tskey-auth-abc123' }),
            }),
        );
    });
});
