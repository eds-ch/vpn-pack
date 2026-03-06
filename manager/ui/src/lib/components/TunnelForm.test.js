import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../api.js', () => ({
    wgS2sGenerateKeypair: vi.fn(),
    wgS2sGetLocalSubnets: vi.fn(),
    wgS2sGetWanIP: vi.fn(),
    wgS2sCreateTunnel: vi.fn(),
    wgS2sListZones: vi.fn(),
}));

vi.mock('../stores/tailscale.svelte.js', () => ({
    addError: vi.fn(),
}));

import {
    wgS2sGenerateKeypair,
    wgS2sGetLocalSubnets,
    wgS2sGetWanIP,
    wgS2sCreateTunnel,
} from '../api.js';
import TunnelForm from './TunnelForm.svelte';

describe('TunnelForm', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        wgS2sGenerateKeypair.mockResolvedValue({
            publicKey: 'test-pub-key-abc123',
            privateKey: 'test-priv-key-xyz',
        });
        wgS2sGetLocalSubnets.mockResolvedValue([]);
        wgS2sGetWanIP.mockResolvedValue({ ip: '1.2.3.4' });
    });

    it('renders form elements after mount', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByText(/new wireguard site-to-site tunnel/i)).toBeInTheDocument();
        });

        expect(screen.getByPlaceholderText('office-nyc')).toBeInTheDocument();
        expect(screen.getByPlaceholderText('10.255.0.1/30')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
    });

    it('shows public key after keypair loads', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByDisplayValue('test-pub-key-abc123')).toBeInTheDocument();
        });
    });

    it('shows validation errors on empty form submission', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            const errors = screen.getAllByText('Required');
            expect(errors.length).toBeGreaterThanOrEqual(2);
        });
    });

    it('calls onCancel when cancel button clicked', async () => {
        const onCancel = vi.fn();
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel,
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
        });

        await fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
        expect(onCancel).toHaveBeenCalled();
    });

    it('shows port error for invalid port (0)', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        });

        const nameInput = screen.getByPlaceholderText('office-nyc');
        const portInput = screen.getByDisplayValue('51820');
        const addrInput = screen.getByPlaceholderText('10.255.0.1/30');
        const keyInput = screen.getByPlaceholderText(/paste public key/i);

        await fireEvent.input(nameInput, { target: { value: 'test-tunnel' } });
        await fireEvent.input(portInput, { target: { value: '0' } });
        await fireEvent.input(addrInput, { target: { value: '10.255.0.1/30' } });
        await fireEvent.input(keyInput, { target: { value: 'validkey123' } });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            expect(screen.getByText('Must be 1-65535')).toBeInTheDocument();
        });
    });

    it('shows CIDR error for invalid tunnel address', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        });

        const nameInput = screen.getByPlaceholderText('office-nyc');
        const addrInput = screen.getByPlaceholderText('10.255.0.1/30');
        const keyInput = screen.getByPlaceholderText(/paste public key/i);

        await fireEvent.input(nameInput, { target: { value: 'test-tunnel' } });
        await fireEvent.input(addrInput, { target: { value: 'not-a-cidr' } });
        await fireEvent.input(keyInput, { target: { value: 'validkey123' } });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            expect(screen.getByText(/invalid cidr/i)).toBeInTheDocument();
        });
    });

    it('shows key error when peer public key is empty', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        });

        const nameInput = screen.getByPlaceholderText('office-nyc');
        const addrInput = screen.getByPlaceholderText('10.255.0.1/30');

        await fireEvent.input(nameInput, { target: { value: 'test-tunnel' } });
        await fireEvent.input(addrInput, { target: { value: '10.255.0.1/30' } });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            const errors = screen.getAllByText('Required');
            expect(errors.length).toBeGreaterThanOrEqual(1);
        });
    });

    it('calls wgS2sCreateTunnel with correct payload on valid submission', async () => {
        const onCreated = vi.fn();
        wgS2sCreateTunnel.mockResolvedValue({ id: 'tun-new' });

        render(TunnelForm, {
            onCreated,
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByDisplayValue('test-pub-key-abc123')).toBeInTheDocument();
        });

        const nameInput = screen.getByPlaceholderText('office-nyc');
        const addrInput = screen.getByPlaceholderText('10.255.0.1/30');
        const keyInput = screen.getByPlaceholderText(/paste public key/i);
        const endpointInput = screen.getByPlaceholderText('85.12.34.56:51820');

        await fireEvent.input(nameInput, { target: { value: 'my-tunnel' } });
        await fireEvent.input(addrInput, { target: { value: '10.255.0.1/30' } });
        await fireEvent.input(keyInput, { target: { value: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' } });
        await fireEvent.input(endpointInput, { target: { value: '85.12.34.56:51820' } });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            expect(wgS2sCreateTunnel).toHaveBeenCalledTimes(1);
        });

        const payload = wgS2sCreateTunnel.mock.calls[0][0];
        expect(payload.name).toBe('my-tunnel');
        expect(payload.listenPort).toBe(51820);
        expect(payload.tunnelAddress).toBe('10.255.0.1/30');
        expect(payload.peerPublicKey).toBe('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=');
        expect(payload.peerEndpoint).toBe('85.12.34.56:51820');
        expect(payload.privateKey).toBe('test-priv-key-xyz');
        expect(onCreated).toHaveBeenCalledWith({ id: 'tun-new' });
    });

    it('shows key format error for invalid base64 key', async () => {
        render(TunnelForm, {
            onCreated: vi.fn(),
            onCancel: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create tunnel/i })).toBeInTheDocument();
        });

        const nameInput = screen.getByPlaceholderText('office-nyc');
        const addrInput = screen.getByPlaceholderText('10.255.0.1/30');
        const keyInput = screen.getByPlaceholderText(/paste public key/i);

        await fireEvent.input(nameInput, { target: { value: 'test-tunnel' } });
        await fireEvent.input(addrInput, { target: { value: '10.255.0.1/30' } });
        await fireEvent.input(keyInput, { target: { value: 'not-a-valid-base64-key' } });

        await fireEvent.click(screen.getByRole('button', { name: /create tunnel/i }));

        await waitFor(() => {
            expect(screen.getByText(/44-char base64/i)).toBeInTheDocument();
        });
    });
});
