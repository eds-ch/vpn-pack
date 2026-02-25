import { describe, it, expect, beforeEach, afterEach } from 'vitest';

class MockEventSource {
    constructor(url) {
        this.url = url;
        this.readyState = 0;
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
        this.readyState = 2;
    }
    static instances = [];
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
    });
});
