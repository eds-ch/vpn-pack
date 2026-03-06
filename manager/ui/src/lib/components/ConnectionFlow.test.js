import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';

vi.mock('../api.js', () => ({
    connectWithAuthKey: vi.fn(),
}));

vi.mock('./QRCode.svelte', async () => {
    return import('./__mocks__/QRCode.svelte');
});

import ConnectionFlow from './ConnectionFlow.svelte';

function makeStatus(overrides = {}) {
    return {
        backendState: 'Running',
        tailscaleIPs: ['100.64.0.1'],
        authURL: '',
        connected: true,
        ...overrides,
    };
}

describe('ConnectionFlow', () => {
    it('renders connection heading', () => {
        render(ConnectionFlow, {
            status: makeStatus(),
        });
        expect(screen.getByText('Connection')).toBeInTheDocument();
    });

    it('shows step labels', () => {
        render(ConnectionFlow, {
            status: makeStatus(),
        });
        expect(screen.getByText('Starting tailscaled...')).toBeInTheDocument();
        expect(screen.getByText('Login required')).toBeInTheDocument();
        expect(screen.getByText('Connecting to tailnet...')).toBeInTheDocument();
        expect(screen.getByText('Connected')).toBeInTheDocument();
    });

    it('shows auth options when NeedsLogin with authURL', () => {
        render(ConnectionFlow, {
            status: makeStatus({
                backendState: 'NeedsLogin',
                authURL: 'https://login.tailscale.com/a/abc123',
            }),
        });
        expect(screen.getByText('Login with browser')).toBeInTheDocument();
        expect(screen.getByText('Connect with auth key')).toBeInTheDocument();
        const authLinks = screen.getAllByText('https://login.tailscale.com/a/abc123');
        expect(authLinks.length).toBeGreaterThanOrEqual(1);
    });

    it('does not show auth section when Running', () => {
        render(ConnectionFlow, {
            status: makeStatus({ backendState: 'Running' }),
        });
        expect(screen.queryByText('Login with browser')).not.toBeInTheDocument();
    });

    it('shows waiting message when NeedsLogin without authURL', () => {
        render(ConnectionFlow, {
            status: makeStatus({
                backendState: 'NeedsLogin',
                authURL: '',
            }),
        });
        expect(screen.getByText(/waiting for auth url/i)).toBeInTheDocument();
    });
});
