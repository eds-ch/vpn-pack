import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

import StatusPill from './StatusPill.svelte';

function makeStatus(overrides = {}) {
    return {
        backendState: 'Running',
        tailscaleIPs: ['100.64.0.1'],
        connected: true,
        firewallHealth: null,
        health: [],
        ...overrides,
    };
}

describe('StatusPill', () => {
    it('renders Running state text', () => {
        render(StatusPill, {
            status: makeStatus(),
        });
        expect(screen.getByText(/Running/)).toBeInTheDocument();
    });

    it('renders NeedsLogin as a human label', () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'NeedsLogin' }),
        });
        expect(screen.getByText(/Needs Login/)).toBeInTheDocument();
        expect(screen.queryByText(/NeedsLogin/)).not.toBeInTheDocument();
    });

    it('renders NoState as Connecting, never as the raw enum name', () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'NoState', tailscaleIPs: [] }),
        });
        expect(screen.getByText(/Connecting/)).toBeInTheDocument();
        expect(screen.queryByText(/NoState/)).not.toBeInTheDocument();
    });

    it('shows IP from tailscaleIPs', () => {
        render(StatusPill, {
            status: makeStatus({ tailscaleIPs: ['100.100.1.2'] }),
        });
        expect(screen.getByText('100.100.1.2')).toBeInTheDocument();
    });

    it('renders Stopped state', () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'Stopped', tailscaleIPs: [] }),
        });
        expect(screen.getByText(/Stopped/)).toBeInTheDocument();
    });

    it('does not show IP when tailscaleIPs is empty', () => {
        render(StatusPill, {
            status: makeStatus({ tailscaleIPs: [] }),
        });
        expect(screen.queryByText('100.64.0.1')).not.toBeInTheDocument();
    });

    it('shows health issue count when firewallHealth has issues', () => {
        render(StatusPill, {
            status: makeStatus({
                firewallHealth: {
                    zoneActive: true,
                    watcherRunning: false,
                    udapiReachable: true,
                    chainPrefix: 'VPN',
                },
            }),
        });
        expect(screen.getByText('1')).toBeInTheDocument();
    });

    it('shows health details with correct severity in popover', async () => {
        render(StatusPill, {
            status: makeStatus({
                firewallHealth: {
                    zoneActive: false,
                    watcherRunning: false,
                    udapiReachable: true,
                    chainPrefix: 'VPN',
                },
            }),
        });

        expect(screen.getByText('2')).toBeInTheDocument();

        const button = screen.getByRole('button');
        await fireEvent.click(button);

        await waitFor(() => {
            expect(screen.getByText('Integration Health')).toBeInTheDocument();
        });

        expect(screen.getByText('Firewall Zone')).toBeInTheDocument();
        expect(screen.getByText('Not in firewall zone')).toBeInTheDocument();
        expect(screen.getByText('Watcher')).toBeInTheDocument();
        expect(screen.getByText("Rules won't auto-restore")).toBeInTheDocument();
        expect(screen.getByText('UDAPI Socket')).toBeInTheDocument();
        expect(screen.getByText('Socket connected')).toBeInTheDocument();
    });

    it('shows no health issue badge when all checks pass', () => {
        render(StatusPill, {
            status: makeStatus({
                firewallHealth: {
                    zoneActive: true,
                    watcherRunning: true,
                    udapiReachable: true,
                    chainPrefix: 'VPN',
                },
            }),
        });
        expect(screen.queryByText('1')).not.toBeInTheDocument();
        expect(screen.queryByText('2')).not.toBeInTheDocument();
        expect(screen.queryByText('3')).not.toBeInTheDocument();
    });

    it('shows zone name in zone description', async () => {
        render(StatusPill, {
            status: makeStatus({
                firewallHealth: {
                    zoneActive: true,
                    watcherRunning: true,
                    udapiReachable: true,
                    chainPrefix: 'CUSTOM1',
                    zoneName: 'VPN Pack: Tailscale',
                },
            }),
        });

        const button = screen.getByRole('button');
        await fireEvent.click(button);

        await waitFor(() => {
            expect(screen.getByText('tailscale0 in zone VPN Pack: Tailscale')).toBeInTheDocument();
        });
    });

    it('falls back to chainPrefix when zoneName is empty', async () => {
        render(StatusPill, {
            status: makeStatus({
                firewallHealth: {
                    zoneActive: true,
                    watcherRunning: true,
                    udapiReachable: true,
                    chainPrefix: 'CUSTOM1',
                },
            }),
        });

        const button = screen.getByRole('button');
        await fireEvent.click(button);

        await waitFor(() => {
            expect(screen.getByText('tailscale0 in zone CUSTOM1')).toBeInTheDocument();
        });
    });
});

describe('StatusPill Tailscale Health section', () => {
    const outOfSync = {
        code: 'not-in-map-poll',
        title: 'Out of sync',
        text: 'Unable to connect to the Tailscale coordination server to synchronize the state of your tailnet.',
        severity: 'medium',
        impactsConnectivity: true,
    };

    it('shows the reason in the popover when a warning is active', async () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'NoState', health: [outOfSync] }),
        });
        await fireEvent.click(screen.getByRole('button'));
        await waitFor(() => {
            expect(screen.getByText('Tailscale Health')).toBeInTheDocument();
            expect(screen.getByText('Out of sync')).toBeInTheDocument();
        });
    });

    it('shows the waiting reason when the data stream is down', async () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'NoState', connected: false }),
        });
        await fireEvent.click(screen.getByRole('button'));
        await waitFor(() => {
            expect(screen.getByText('Waiting for tailscaled...')).toBeInTheDocument();
        });
    });

    it('omits the section entirely when healthy and connected', async () => {
        render(StatusPill, { status: makeStatus() });
        await fireEvent.click(screen.getByRole('button'));
        await waitFor(() => {
            expect(screen.getByText('Tailnet IP')).toBeInTheDocument();
        });
        expect(screen.queryByText('Tailscale Health')).not.toBeInTheDocument();
    });
});
