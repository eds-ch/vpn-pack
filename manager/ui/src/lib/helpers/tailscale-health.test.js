import { describe, it, expect } from 'vitest';
import { pickPrimaryWarning, waitingReason } from './tailscale-health.js';

const notInMapPoll = {
    code: 'not-in-map-poll',
    title: 'Out of sync',
    text: 'Unable to connect to the Tailscale coordination server to synchronize the state of your tailnet.',
    severity: 'medium',
    impactsConnectivity: true,
};

const warmingUp = {
    code: 'warming-up',
    title: 'Tailscale is starting',
    text: 'Tailscale is starting. Please wait.',
    severity: 'low',
    impactsConnectivity: false,
};

const noDerpHome = {
    code: 'no-derp-home',
    title: 'No home relay server',
    text: 'Tailscale could not connect to any relay server. Check your Internet connection.',
    severity: 'high',
    impactsConnectivity: true,
};

describe('pickPrimaryWarning', () => {
    it('returns null for no warnings', () => {
        expect(pickPrimaryWarning([])).toBeNull();
        expect(pickPrimaryWarning(undefined)).toBeNull();
    });

    it('prefers high severity over medium and low', () => {
        expect(pickPrimaryWarning([warmingUp, notInMapPoll, noDerpHome]).code).toBe('no-derp-home');
    });

    it('prefers medium over low', () => {
        expect(pickPrimaryWarning([warmingUp, notInMapPoll]).code).toBe('not-in-map-poll');
    });

    it('breaks a severity tie on impactsConnectivity', () => {
        const cosmetic = { ...notInMapPoll, code: 'aaa-cosmetic', impactsConnectivity: false };
        expect(pickPrimaryWarning([cosmetic, notInMapPoll]).code).toBe('not-in-map-poll');
    });

    it('breaks a remaining tie alphabetically by code, so the choice is stable', () => {
        const a = { ...notInMapPoll, code: 'aaa' };
        const z = { ...notInMapPoll, code: 'zzz' };
        expect(pickPrimaryWarning([z, a]).code).toBe('aaa');
        expect(pickPrimaryWarning([a, z]).code).toBe('aaa');
    });

    it('does not mutate the input array', () => {
        const input = [warmingUp, noDerpHome];
        pickPrimaryWarning(input);
        expect(input[0].code).toBe('warming-up');
    });

    it('treats an unknown severity as the lowest', () => {
        const weird = { ...warmingUp, code: 'weird', severity: 'chartreuse' };
        expect(pickPrimaryWarning([weird, warmingUp]).code).toBe('warming-up');
    });
});

describe('waitingReason', () => {
    it('says it is waiting for tailscaled only when the data stream is down', () => {
        expect(waitingReason({ connected: false, health: [] }).title)
            .toBe('Waiting for tailscaled...');
    });

    it('reports the active warning when the manager is connected', () => {
        const reason = waitingReason({ connected: true, health: [notInMapPoll] });
        expect(reason.title).toBe('Out of sync');
        expect(reason.text).toBe(notInMapPoll.text);
    });

    it('never claims to be waiting for tailscaled while connected', () => {
        const reason = waitingReason({ connected: true, health: [notInMapPoll] });
        expect(reason.title).not.toMatch(/tailscaled/);
    });

    it('falls back to a neutral line when connected with no warnings', () => {
        const reason = waitingReason({ connected: true, health: [] });
        expect(reason.title).toBe('Connecting to the Tailscale coordination server...');
        expect(reason.text).toBe('');
    });

    it('handles a missing health field (omitempty drops it from the payload)', () => {
        expect(waitingReason({ connected: true }).title)
            .toBe('Connecting to the Tailscale coordination server...');
    });

    it('falls back to the code when a warning carries no title', () => {
        const bare = { code: 'mystery-code', severity: 'low' };
        const reason = waitingReason({ connected: true, health: [bare] });
        expect(reason.title).toBe('mystery-code');
        expect(reason.text).toBe('');
    });
});
