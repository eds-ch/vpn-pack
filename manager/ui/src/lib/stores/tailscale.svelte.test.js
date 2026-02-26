import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

class MockEventSource {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 2;
    static instances = [];

    constructor(url) {
        this.url = url;
        this.readyState = MockEventSource.CONNECTING;
        this.onopen = null;
        this.onmessage = null;
        this.onerror = null;
        MockEventSource.instances.push(this);
    }
    addEventListener(type, handler) {
        if (!this._listeners) this._listeners = {};
        this._listeners[type] = handler;
    }
    close() {
        this.readyState = MockEventSource.CLOSED;
    }
    static reset() {
        MockEventSource.instances = [];
    }
}
globalThis.EventSource = MockEventSource;

import {
    getStatus,
    getErrors,
    getLogs,
    addError,
    dismissError,
    connect,
    disconnect,
} from './tailscale.svelte.js';

describe('tailscale store', () => {
    beforeEach(() => {
        MockEventSource.reset();
        while (getErrors().length > 0) {
            dismissError(getErrors()[0].id);
        }
        getLogs().splice(0, getLogs().length);
        disconnect();
    });

    describe('initial state', () => {
        it('has default backendState', () => {
            const s = getStatus();
            expect(s.backendState).toBe('Unknown');
        });

        it('has empty tailscaleIPs', () => {
            expect(getStatus().tailscaleIPs).toEqual([]);
        });

        it('has empty peers', () => {
            expect(getStatus().peers).toEqual([]);
        });

        it('is not connected', () => {
            expect(getStatus().connected).toBe(false);
        });

        it('has default settings fields', () => {
            const s = getStatus();
            expect(s.hostname).toBe('');
            expect(s.acceptDNS).toBe(false);
            expect(s.acceptRoutes).toBe(false);
            expect(s.shieldsUp).toBe(false);
            expect(s.runSSH).toBe(false);
            expect(s.noSNAT).toBe(false);
            expect(s.udpPort).toBe(0);
            expect(s.relayServerPort).toBe(null);
            expect(s.relayServerEndpoints).toBe('');
            expect(s.advertiseTags).toEqual([]);
        });
    });

    describe('addError / dismissError', () => {
        it('adds error with id and timestamp', () => {
            addError('something broke');
            const errs = getErrors();
            expect(errs.length).toBeGreaterThanOrEqual(1);
            const last = errs[errs.length - 1];
            expect(last.message).toBe('something broke');
            expect(last.id).toBeTypeOf('number');
            expect(last.timestamp).toBeTruthy();
        });

        it('dismisses error by id', () => {
            addError('err1');
            const errs = getErrors();
            const id = errs[errs.length - 1].id;
            const before = errs.length;
            dismissError(id);
            expect(getErrors().length).toBe(before - 1);
        });

        it('dismissError is a no-op for nonexistent id', () => {
            const before = getErrors().length;
            dismissError(999999);
            expect(getErrors().length).toBe(before);
        });
    });

    describe('connect / disconnect', () => {
        it('creates EventSource on connect', () => {
            connect();
            expect(MockEventSource.instances.length).toBeGreaterThanOrEqual(1);
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];
            expect(es.url).toBe('/vpn-pack/api/events');
        });

        it('updates status on SSE message', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];
            es.onmessage({
                data: JSON.stringify({
                    backendState: 'Running',
                    tailscaleIPs: ['100.64.0.1'],
                }),
            });
            expect(getStatus().backendState).toBe('Running');
            expect(getStatus().tailscaleIPs).toEqual(['100.64.0.1']);
        });

        it('sets connected=false on disconnect', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];
            if (es.onopen) es.onopen();
            expect(getStatus().connected).toBe(true);

            disconnect();
            expect(getStatus().connected).toBe(false);
        });

        it('closes EventSource on disconnect', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];
            disconnect();
            expect(es.readyState).toBe(2);
        });

        it('SSE onmessage updates specific status fields individually', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];

            const ipsBefore = JSON.stringify(getStatus().tailscaleIPs);

            es.onmessage({
                data: JSON.stringify({ backendState: 'NeedsLogin' }),
            });
            expect(getStatus().backendState).toBe('NeedsLogin');
            expect(JSON.stringify(getStatus().tailscaleIPs)).toBe(ipsBefore);

            es.onmessage({
                data: JSON.stringify({ tailscaleIPs: ['100.64.0.99'] }),
            });
            expect(getStatus().backendState).toBe('NeedsLogin');
            expect(getStatus().tailscaleIPs).toEqual(['100.64.0.99']);
        });

        it('logs state changes when backendState transitions', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];

            const logsBefore = getLogs().length;
            const currentState = getStatus().backendState;

            const newState = currentState === 'Stopped' ? 'Running' : 'Stopped';
            es.onmessage({
                data: JSON.stringify({ backendState: newState }),
            });

            const newLogs = getLogs().slice(0, getLogs().length - logsBefore);
            const stateLog = newLogs.find(l => l.message.includes('State changed'));
            expect(stateLog).toBeTruthy();
            expect(stateLog.message).toContain(currentState);
            expect(stateLog.message).toContain(newState);
            expect(stateLog.level).toBe('info');
        });

        it('invalid JSON in SSE message does not crash and logs error', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];

            const logsBefore = getLogs().length;

            expect(() => {
                es.onmessage({ data: '{broken json!!!' });
            }).not.toThrow();

            const newLogs = getLogs().slice(0, getLogs().length - logsBefore);
            const errorLog = newLogs.find(l => l.message.includes('Failed to parse SSE'));
            expect(errorLog).toBeTruthy();
            expect(errorLog.level).toBe('error');
        });

        it('reconnects when EventSource enters CLOSED state', () => {
            vi.useFakeTimers();
            try {
                connect();
                const es = MockEventSource.instances[MockEventSource.instances.length - 1];
                const countBefore = MockEventSource.instances.length;

                es.readyState = MockEventSource.CLOSED;
                es.onerror();

                expect(getStatus().connected).toBe(false);
                expect(MockEventSource.instances.length).toBe(countBefore);

                vi.advanceTimersByTime(3000);

                expect(MockEventSource.instances.length).toBe(countBefore + 1);
                const newEs = MockEventSource.instances[MockEventSource.instances.length - 1];
                expect(newEs.url).toBe('/vpn-pack/api/events');
            } finally {
                vi.useRealTimers();
            }
        });

        it('does not reconnect when EventSource is still CONNECTING', () => {
            vi.useFakeTimers();
            try {
                connect();
                const es = MockEventSource.instances[MockEventSource.instances.length - 1];
                const countBefore = MockEventSource.instances.length;

                es.readyState = MockEventSource.CONNECTING;
                es.onerror();

                vi.advanceTimersByTime(10000);

                expect(MockEventSource.instances.length).toBe(countBefore);
            } finally {
                vi.useRealTimers();
            }
        });

        it('updates settings fields on SSE message', () => {
            connect();
            const es = MockEventSource.instances[MockEventSource.instances.length - 1];
            es.onmessage({
                data: JSON.stringify({
                    hostname: 'new-host',
                    acceptRoutes: true,
                    shieldsUp: true,
                    udpPort: 51820,
                    advertiseTags: ['tag:relay'],
                }),
            });
            expect(getStatus().hostname).toBe('new-host');
            expect(getStatus().acceptRoutes).toBe(true);
            expect(getStatus().shieldsUp).toBe(true);
            expect(getStatus().udpPort).toBe(51820);
            expect(getStatus().advertiseTags).toEqual(['tag:relay']);
        });

        it('disconnect cancels pending reconnect timer', () => {
            vi.useFakeTimers();
            try {
                connect();
                const es = MockEventSource.instances[MockEventSource.instances.length - 1];
                const countBefore = MockEventSource.instances.length;

                es.readyState = MockEventSource.CLOSED;
                es.onerror();

                disconnect();
                vi.advanceTimersByTime(5000);

                expect(MockEventSource.instances.length).toBe(countBefore);
            } finally {
                vi.useRealTimers();
            }
        });
    });
});
