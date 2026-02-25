import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { SvelteSet } from 'svelte/reactivity';
import StatusPill from './StatusPill.svelte';

function makeStatus(overrides = {}) {
    return {
        backendState: 'Running',
        tailscaleIPs: ['100.64.0.1'],
        connected: true,
        firewallHealth: null,
        ...overrides,
    };
}

describe('StatusPill', () => {
    it('renders Running state text', () => {
        render(StatusPill, {
            status: makeStatus(),
            changedFields: new SvelteSet(),
        });
        expect(screen.getByText(/Running/)).toBeInTheDocument();
    });

    it('renders NeedsLogin state text', () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'NeedsLogin' }),
            changedFields: new SvelteSet(),
        });
        expect(screen.getByText(/NeedsLogin/)).toBeInTheDocument();
    });

    it('shows IP from tailscaleIPs', () => {
        render(StatusPill, {
            status: makeStatus({ tailscaleIPs: ['100.100.1.2'] }),
            changedFields: new SvelteSet(),
        });
        expect(screen.getByText('100.100.1.2')).toBeInTheDocument();
    });

    it('renders Stopped state', () => {
        render(StatusPill, {
            status: makeStatus({ backendState: 'Stopped', tailscaleIPs: [] }),
            changedFields: new SvelteSet(),
        });
        expect(screen.getByText(/Stopped/)).toBeInTheDocument();
    });

    it('does not show IP when tailscaleIPs is empty', () => {
        render(StatusPill, {
            status: makeStatus({ tailscaleIPs: [] }),
            changedFields: new SvelteSet(),
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
                },
            }),
            changedFields: new SvelteSet(),
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
                },
            }),
            changedFields: new SvelteSet(),
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
                },
            }),
            changedFields: new SvelteSet(),
        });
        expect(screen.queryByText('1')).not.toBeInTheDocument();
        expect(screen.queryByText('2')).not.toBeInTheDocument();
        expect(screen.queryByText('3')).not.toBeInTheDocument();
    });
});
