import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('../api.js', () => ({
    wgS2sUpdateTunnel: vi.fn(),
    wgS2sDeleteTunnel: vi.fn(),
    wgS2sEnableTunnel: vi.fn(),
    wgS2sDisableTunnel: vi.fn(),
    wgS2sGetConfig: vi.fn(),
}));

import { wgS2sUpdateTunnel, wgS2sDeleteTunnel } from '../api.js';
import TunnelCard from './TunnelCard.svelte';

function makeTunnel(overrides = {}) {
    return {
        id: 'tun-1',
        name: 'office-nyc',
        listenPort: 51820,
        tunnelAddress: '10.255.0.1/30',
        peerPublicKey: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
        peerEndpoint: '85.12.34.56:51820',
        allowedIPs: ['10.20.0.0/24'],
        persistentKeepalive: 25,
        mtu: 1420,
        connected: true,
        enabled: true,
        lastHandshake: '2026-02-24T12:00:00Z',
        transferTx: 1048576,
        transferRx: 2097152,
        interfaceName: 'wgs2s0',
        ...overrides,
    };
}

describe('TunnelCard', () => {
    it('renders tunnel name', () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText('office-nyc')).toBeInTheDocument();
    });

    it('shows Connected status for connected tunnel', () => {
        render(TunnelCard, {
            tunnel: makeTunnel({ connected: true }),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText('Connected')).toBeInTheDocument();
    });

    it('shows No handshake for disconnected tunnel without handshake', () => {
        render(TunnelCard, {
            tunnel: makeTunnel({
                connected: false,
                lastHandshake: '0001-01-01T00:00:00Z',
            }),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText('No handshake')).toBeInTheDocument();
    });

    it('shows Handshake stale for disconnected tunnel with old handshake', () => {
        render(TunnelCard, {
            tunnel: makeTunnel({
                connected: false,
                lastHandshake: '2026-02-20T12:00:00Z',
            }),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText('Handshake stale')).toBeInTheDocument();
    });

    it('shows Disabled badge when tunnel is disabled', () => {
        render(TunnelCard, {
            tunnel: makeTunnel({ enabled: false }),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText('Disabled')).toBeInTheDocument();
    });

    it('shows transfer stats', () => {
        render(TunnelCard, {
            tunnel: makeTunnel({ transferTx: 1048576, transferRx: 2097152 }),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByText(/TX 1\.0 MB/)).toBeInTheDocument();
        expect(screen.getByText(/RX 2\.0 MB/)).toBeInTheDocument();
    });

    it('renders action buttons', () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });
        expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /disable/i })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument();
    });

    it('shows edit form when edit button is clicked', async () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });

        await fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /apply/i })).toBeInTheDocument();
        });
        expect(screen.getByDisplayValue('office-nyc')).toBeInTheDocument();
        expect(screen.getByDisplayValue('51820')).toBeInTheDocument();
        expect(screen.getByDisplayValue('10.255.0.1/30')).toBeInTheDocument();
        expect(screen.getByDisplayValue('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=')).toBeInTheDocument();
    });

    it('shows validation errors when applying edit with empty name', async () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });

        await fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /apply/i })).toBeInTheDocument();
        });

        const nameInput = screen.getByDisplayValue('office-nyc');
        await fireEvent.input(nameInput, { target: { value: '' } });

        await fireEvent.click(screen.getByRole('button', { name: /apply/i }));

        await waitFor(() => {
            expect(screen.getByText('Required')).toBeInTheDocument();
        });
        expect(wgS2sUpdateTunnel).not.toHaveBeenCalled();
    });

    it('shows validation error for invalid CIDR in edit mode', async () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });

        await fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /apply/i })).toBeInTheDocument();
        });

        const addrInput = screen.getByDisplayValue('10.255.0.1/30');
        await fireEvent.input(addrInput, { target: { value: 'not-cidr' } });

        await fireEvent.click(screen.getByRole('button', { name: /apply/i }));

        await waitFor(() => {
            expect(screen.getByText(/invalid cidr/i)).toBeInTheDocument();
        });
        expect(wgS2sUpdateTunnel).not.toHaveBeenCalled();
    });

    it('calls wgS2sUpdateTunnel on valid edit submission', async () => {
        const onUpdate = vi.fn();
        wgS2sUpdateTunnel.mockResolvedValue({ ok: true });

        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate,
            onDelete: vi.fn(),
        });

        await fireEvent.click(screen.getByRole('button', { name: /^edit$/i }));

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /apply/i })).toBeInTheDocument();
        });

        await fireEvent.click(screen.getByRole('button', { name: /apply/i }));

        await waitFor(() => {
            expect(wgS2sUpdateTunnel).toHaveBeenCalledTimes(1);
        });
        expect(onUpdate).toHaveBeenCalled();
    });

    it('shows delete confirmation when delete button is clicked', async () => {
        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete: vi.fn(),
        });

        await fireEvent.click(screen.getByRole('button', { name: /^delete$/i }));

        await waitFor(() => {
            expect(screen.getByText(/are you sure/i)).toBeInTheDocument();
        });
    });

    it('calls wgS2sDeleteTunnel on confirmed delete', async () => {
        const onDelete = vi.fn();
        wgS2sDeleteTunnel.mockResolvedValue({ ok: true });

        render(TunnelCard, {
            tunnel: makeTunnel(),
            onUpdate: vi.fn(),
            onDelete,
        });

        await fireEvent.click(screen.getByRole('button', { name: /^delete$/i }));

        await waitFor(() => {
            expect(screen.getByText(/are you sure/i)).toBeInTheDocument();
        });

        const confirmButtons = screen.getAllByRole('button', { name: /^delete$/i });
        const confirmBtn = confirmButtons.find(btn =>
            btn.closest('.bg-error\\/10') || btn.classList.contains('bg-error')
        );
        await fireEvent.click(confirmBtn);

        await waitFor(() => {
            expect(wgS2sDeleteTunnel).toHaveBeenCalledWith('tun-1');
        });
        expect(onDelete).toHaveBeenCalled();
    });
});
