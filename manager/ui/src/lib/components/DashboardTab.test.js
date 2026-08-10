import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';

// DashboardTab calls getDiagnostics() from onMount. Spread the real module so
// the sibling components that also import from api.js keep their exports.
vi.mock('../api.js', async (importOriginal) => ({
    ...(await importOriginal()),
    getDiagnostics: vi.fn(async () => null),
}));

import DashboardTab from './DashboardTab.svelte';

function makeStatus(overrides = {}) {
    return {
        backendState: 'NoState',
        tailscaleIPs: [],
        connected: true,
        health: [],
        integrationStatus: { configured: true },
        wgS2sTunnels: [],
        ...overrides,
    };
}

describe('DashboardTab waiting branch', () => {
    it('does not claim to wait for tailscaled while the data stream is up', () => {
        render(DashboardTab, { status: makeStatus(), deviceInfo: null });
        expect(screen.queryByText(/Waiting for tailscaled/)).not.toBeInTheDocument();
    });

    it('says it is waiting for tailscaled when the data stream is down', () => {
        render(DashboardTab, {
            status: makeStatus({ connected: false }),
            deviceInfo: null,
        });
        expect(screen.getByText('Waiting for tailscaled...')).toBeInTheDocument();
    });

    it('names the active health warning in NoState', () => {
        render(DashboardTab, {
            status: makeStatus({
                health: [{
                    code: 'not-in-map-poll',
                    title: 'Out of sync',
                    text: 'Unable to connect to the Tailscale coordination server to synchronize the state of your tailnet.',
                    severity: 'medium',
                    impactsConnectivity: true,
                }],
            }),
            deviceInfo: null,
        });
        expect(screen.getByText('Out of sync')).toBeInTheDocument();
        expect(screen.getByText(/synchronize the state of your tailnet/)).toBeInTheDocument();
    });

    it('shows a neutral line when connected with no warnings', () => {
        render(DashboardTab, { status: makeStatus(), deviceInfo: null });
        expect(
            screen.getByText('Connecting to the Tailscale coordination server...'),
        ).toBeInTheDocument();
    });
});
