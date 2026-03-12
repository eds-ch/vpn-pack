import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

import ExitNodeToggle from './ExitNodeToggle.svelte';

describe('ExitNodeToggle', () => {
    it('renders title and description', () => {
        render(ExitNodeToggle, {
            enabled: false,
            onchange: vi.fn(),
        });

        expect(screen.getByText('Advertise as Exit Node')).toBeInTheDocument();
        expect(screen.getByText(/Your LAN clients are not affected/)).toBeInTheDocument();
    });

    it('renders toggle in off state', () => {
        render(ExitNodeToggle, {
            enabled: false,
            onchange: vi.fn(),
        });

        const checkbox = screen.getByRole('checkbox');
        expect(checkbox.checked).toBe(false);
    });

    it('renders toggle in on state', () => {
        render(ExitNodeToggle, {
            enabled: true,
            onchange: vi.fn(),
        });

        const checkbox = screen.getByRole('checkbox');
        expect(checkbox.checked).toBe(true);
    });

    it('calls onchange with toggled value on click', async () => {
        const onchange = vi.fn();
        render(ExitNodeToggle, {
            enabled: false,
            onchange,
        });

        const checkbox = screen.getByRole('checkbox');
        await fireEvent.click(checkbox);
        expect(onchange).toHaveBeenCalledWith(true);
    });

    it('calls onchange with false when disabling', async () => {
        const onchange = vi.fn();
        render(ExitNodeToggle, {
            enabled: true,
            onchange,
        });

        const checkbox = screen.getByRole('checkbox');
        await fireEvent.click(checkbox);
        expect(onchange).toHaveBeenCalledWith(false);
    });

    it('shows VPN client warning when enabled with active clients', () => {
        render(ExitNodeToggle, {
            enabled: true,
            activeVPNClients: ['wgclt1', 'wgclt2'],
            onchange: vi.fn(),
        });

        expect(screen.getByText(/wgclt1/)).toBeInTheDocument();
        expect(screen.getByText(/wgclt2/)).toBeInTheDocument();
    });

    it('hides VPN client warning when disabled', () => {
        render(ExitNodeToggle, {
            enabled: false,
            activeVPNClients: ['wgclt1'],
            onchange: vi.fn(),
        });

        expect(screen.queryByText(/wgclt1/)).not.toBeInTheDocument();
    });

    it('shows DPI warning when dpiFingerprinting is false', () => {
        render(ExitNodeToggle, {
            enabled: true,
            dpiFingerprinting: false,
            onchange: vi.fn(),
        });

        expect(screen.getByText(/DPI fingerprinting is disabled/)).toBeInTheDocument();
    });

    it('hides DPI warning when dpiFingerprinting is null', () => {
        render(ExitNodeToggle, {
            enabled: true,
            dpiFingerprinting: null,
            onchange: vi.fn(),
        });

        expect(screen.queryByText(/DPI fingerprinting/)).not.toBeInTheDocument();
    });

    it('does not render mode selector or client picker', () => {
        render(ExitNodeToggle, {
            enabled: true,
            onchange: vi.fn(),
        });

        expect(screen.queryByText(/All traffic/)).not.toBeInTheDocument();
        expect(screen.queryByText(/Selected clients/)).not.toBeInTheDocument();
    });
});
