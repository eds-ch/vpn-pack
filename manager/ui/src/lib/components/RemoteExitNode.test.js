import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('../api.js', () => ({
    getRemoteExitNode: vi.fn(),
    enableRemoteExitNode: vi.fn(),
    disableRemoteExitNode: vi.fn(),
}));

import { getRemoteExitNode, enableRemoteExitNode, disableRemoteExitNode } from '../api.js';
import RemoteExitNode from './RemoteExitNode.svelte';

const onlinePeer = { id: 'stable-1', hostName: 'exit-server', dnsName: 'exit-server.ts.net.', online: true, os: 'linux', active: false };
const offlinePeer = { id: 'stable-2', hostName: 'backup-exit', dnsName: 'backup-exit.ts.net.', online: false, os: 'linux', active: false };

describe('RemoteExitNode', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders title and description', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode);

        expect(screen.getByText('Use Remote Exit Node')).toBeInTheDocument();
        expect(screen.getByText(/Route this router.*internet traffic/)).toBeInTheDocument();
    });

    it('shows "no peers" message when list is empty', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode);

        await waitFor(() => {
            expect(screen.getByText(/No exit node peers available/)).toBeInTheDocument();
        });
    });

    it('shows peer selector with online/offline groups', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer, offlinePeer], current: null });

        render(RemoteExitNode);

        await waitFor(() => {
            expect(screen.getByText(/Select a peer/)).toBeInTheDocument();
        });

        const select = screen.getByRole('combobox');
        const options = select.querySelectorAll('option');
        expect(options.length).toBeGreaterThanOrEqual(3); // placeholder + 2 peers
    });

    it('shows mode selector after peer is selected', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });

        render(RemoteExitNode);

        await waitFor(() => {
            expect(screen.getByRole('combobox')).toBeInTheDocument();
        });

        const select = screen.getByRole('combobox');
        await fireEvent.change(select, { target: { value: 'stable-1' } });

        expect(screen.getByText('All traffic')).toBeInTheDocument();
        expect(screen.getByText('Selected clients')).toBeInTheDocument();
    });

    it('sends enable request with confirmation gate for mode "all"', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });
        enableRemoteExitNode.mockResolvedValueOnce({
            confirmRequired: true,
            message: 'All internet traffic from ALL clients will be routed through exit-server.',
        });

        render(RemoteExitNode);

        await waitFor(() => {
            expect(screen.getByRole('combobox')).toBeInTheDocument();
        });

        const select = screen.getByRole('combobox');
        await fireEvent.change(select, { target: { value: 'stable-1' } });

        const enableBtn = screen.getByRole('button', { name: 'Enable' });
        await fireEvent.click(enableBtn);

        expect(enableRemoteExitNode).toHaveBeenCalledWith({
            peerId: 'stable-1',
            mode: 'all',
            clients: undefined,
            confirm: false,
        });

        await waitFor(() => {
            expect(screen.getByText(/ALL clients/)).toBeInTheDocument();
        });

        expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
    });

    it('sends confirmed request after user confirms', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });
        enableRemoteExitNode
            .mockResolvedValueOnce({
                confirmRequired: true,
                message: 'All traffic will be routed.',
            })
            .mockResolvedValueOnce({
                ok: true,
                message: 'Traffic routed through exit-server.',
            });

        render(RemoteExitNode);

        await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument());

        await fireEvent.change(screen.getByRole('combobox'), { target: { value: 'stable-1' } });
        await fireEvent.click(screen.getByRole('button', { name: 'Enable' }));

        await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument());

        await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

        expect(enableRemoteExitNode).toHaveBeenCalledTimes(2);
        expect(enableRemoteExitNode).toHaveBeenLastCalledWith({
            peerId: 'stable-1',
            mode: 'all',
            clients: undefined,
            confirm: true,
        });
    });

    it('cancels confirmation and resets state', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });
        enableRemoteExitNode.mockResolvedValueOnce({
            confirmRequired: true,
            message: 'Warning text.',
        });

        render(RemoteExitNode);

        await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument());

        await fireEvent.change(screen.getByRole('combobox'), { target: { value: 'stable-1' } });
        await fireEvent.click(screen.getByRole('button', { name: 'Enable' }));

        await waitFor(() => expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument());
        await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(screen.queryByText('Warning text.')).not.toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Enable' })).toBeInTheDocument();
    });

    it('shows active exit node status with disable button', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: true, mode: 'all' },
        });

        expect(screen.getByText('exit-server')).toBeInTheDocument();
        expect(screen.getByText('All traffic')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument();
    });

    it('calls disable API and refreshes on disable', async () => {
        getRemoteExitNode.mockResolvedValue({ peers: [onlinePeer], current: null });
        disableRemoteExitNode.mockResolvedValue({ ok: true });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: true, mode: 'all' },
        });

        const disableBtn = screen.getByRole('button', { name: 'Disable' });
        await fireEvent.click(disableBtn);

        expect(disableRemoteExitNode).toHaveBeenCalled();
    });

    it('shows offline warning when current exit node is offline', () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: false, mode: 'all' },
        });

        expect(screen.getByText(/Exit node is offline/)).toBeInTheDocument();
    });

    it('shows routing loop warning when advertise is enabled', () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: true, mode: 'all' },
            advertiseEnabled: true,
        });

        expect(screen.getByText(/routing loop/)).toBeInTheDocument();
    });

    it('does not show routing loop warning when advertise is disabled', () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: true, mode: 'all' },
            advertiseEnabled: false,
        });

        expect(screen.queryByText(/routing loop/)).not.toBeInTheDocument();
    });

    it('shows "Selected clients" label for selective mode', () => {
        getRemoteExitNode.mockResolvedValue({ peers: [], current: null });

        render(RemoteExitNode, {
            current: { peerId: 'stable-1', hostName: 'exit-server', online: true, mode: 'selective' },
        });

        expect(screen.getByText('Selected clients')).toBeInTheDocument();
    });
});
