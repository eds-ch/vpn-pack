import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

import RemoteExitNode from './RemoteExitNode.svelte';

const onlinePeer = { id: 'stable-1', hostName: 'exit-server', dnsName: 'exit-server.ts.net.', online: true, os: 'linux', active: false };
const offlinePeer = { id: 'stable-2', hostName: 'backup-exit', dnsName: 'backup-exit.ts.net.', online: false, os: 'linux', active: false };

describe('RemoteExitNode', () => {
    let ontoggle, onpeerchange, onmodechange, onclientschange;

    beforeEach(() => {
        ontoggle = vi.fn();
        onpeerchange = vi.fn();
        onmodechange = vi.fn();
        onclientschange = vi.fn();
    });

    function renderWith(props = {}) {
        return render(RemoteExitNode, {
            peers: [],
            peersLoading: false,
            current: null,
            enabled: false,
            selectedPeerId: '',
            mode: 'all',
            clients: [],
            ontoggle,
            onpeerchange,
            onmodechange,
            onclientschange,
            ...props,
        });
    }

    it('renders title and description', () => {
        renderWith();
        expect(screen.getByText('Use Remote Exit Node')).toBeInTheDocument();
        expect(screen.getByText(/Route LAN clients/)).toBeInTheDocument();
    });

    it('shows toggle in off state by default', () => {
        renderWith();
        const checkbox = screen.getByRole('checkbox');
        expect(checkbox.checked).toBe(false);
    });

    it('does not show form when disabled', () => {
        renderWith({ peers: [onlinePeer] });
        expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    });

    it('calls ontoggle when toggle is clicked', async () => {
        renderWith();
        await fireEvent.click(screen.getByRole('checkbox'));
        expect(ontoggle).toHaveBeenCalledWith(true);
    });

    it('shows "no peers" message when enabled with empty list', () => {
        renderWith({ enabled: true, peers: [] });
        expect(screen.getByText(/No exit node peers available/)).toBeInTheDocument();
    });

    it('shows loading skeleton when peersLoading', () => {
        const { container } = renderWith({ enabled: true, peersLoading: true });
        expect(container.querySelector('.animate-pulse')).toBeInTheDocument();
    });

    it('shows peer selector with online/offline groups', () => {
        renderWith({ enabled: true, peers: [onlinePeer, offlinePeer] });

        expect(screen.getByText(/Select a peer/)).toBeInTheDocument();
        const select = screen.getByRole('combobox');
        const options = select.querySelectorAll('option');
        expect(options.length).toBeGreaterThanOrEqual(3);
    });

    it('calls onpeerchange when peer is selected', async () => {
        renderWith({ enabled: true, peers: [onlinePeer] });

        const select = screen.getByRole('combobox');
        await fireEvent.change(select, { target: { value: 'stable-1' } });

        expect(onpeerchange).toHaveBeenCalledWith('stable-1');
    });

    it('shows mode selector when peer is selected', () => {
        renderWith({ enabled: true, peers: [onlinePeer], selectedPeerId: 'stable-1' });

        expect(screen.getByText('All traffic')).toBeInTheDocument();
        expect(screen.getByText('Selected clients')).toBeInTheDocument();
    });

    it('calls onmodechange when mode button is clicked', async () => {
        renderWith({ enabled: true, peers: [onlinePeer], selectedPeerId: 'stable-1' });

        await fireEvent.click(screen.getByText('Selected clients'));
        expect(onmodechange).toHaveBeenCalledWith('selective');
    });

    it('shows client input in selective mode', () => {
        renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
            mode: 'selective',
        });

        expect(screen.getByPlaceholderText(/192\.168/)).toBeInTheDocument();
    });

    it('calls onclientschange when adding a client', async () => {
        renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
            mode: 'selective',
        });

        const ipInput = screen.getByPlaceholderText(/192\.168/);
        await fireEvent.input(ipInput, { target: { value: '10.0.0.1' } });
        await fireEvent.click(screen.getByText('Add'));

        expect(onclientschange).toHaveBeenCalledWith([{ ip: '10.0.0.1', label: undefined }]);
    });

    it('calls onclientschange when removing a client chip', async () => {
        renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
            mode: 'selective',
            clients: [{ ip: '10.0.0.1' }, { ip: '10.0.0.2' }],
        });

        const removeButtons = screen.getAllByText('×');
        await fireEvent.click(removeButtons[0]);

        expect(onclientschange).toHaveBeenCalledWith([{ ip: '10.0.0.2' }]);
    });

    it('shows validation error for invalid IP', async () => {
        renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
            mode: 'selective',
        });

        const ipInput = screen.getByPlaceholderText(/192\.168/);
        await fireEvent.input(ipInput, { target: { value: 'not-an-ip' } });
        await fireEvent.click(screen.getByText('Add'));

        expect(screen.getByText(/Invalid IP or CIDR/)).toBeInTheDocument();
        expect(onclientschange).not.toHaveBeenCalled();
    });

    it('shows online status dot when peer is selected', () => {
        const { container } = renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
        });

        expect(container.querySelector('.bg-success')).toBeInTheDocument();
    });

    it('shows offline status dot and warning when selected peer is offline', () => {
        const { container } = renderWith({
            enabled: true,
            peers: [offlinePeer],
            selectedPeerId: 'stable-2',
            current: { peerId: 'stable-2', hostName: 'backup-exit', online: false, mode: 'all' },
        });

        expect(container.querySelector('.bg-error')).toBeInTheDocument();
        expect(screen.getByText(/Exit node is offline/)).toBeInTheDocument();
    });

    it('shows mode buttons when peer is selected', () => {
        renderWith({
            enabled: true,
            peers: [onlinePeer],
            selectedPeerId: 'stable-1',
        });

        expect(screen.getByText('All traffic')).toBeInTheDocument();
        expect(screen.getByText('Selected clients')).toBeInTheDocument();
    });
});
