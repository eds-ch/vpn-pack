import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../api.js', () => ({
    getSubnets: vi.fn(),
}));

vi.mock('./Icon.svelte', () => ({
    default: {
        $$: { on_mount: [], on_destroy: [], before_update: [], after_update: [], context: new Map() },
        render: () => ({ html: '<span data-testid="icon"></span>', css: { code: '', map: null }, head: '' }),
    },
}));

import { getSubnets } from '../api.js';
import SubnetPicker from './SubnetPicker.svelte';

describe('SubnetPicker', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders after async load with detected subnets', async () => {
        getSubnets.mockResolvedValue({
            subnets: [{ cidr: '192.168.1.0/24', name: 'LAN' }],
        });

        render(SubnetPicker, {
            value: [],
            routes: [],
            onchange: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByText('192.168.1.0/24')).toBeInTheDocument();
        });
        expect(screen.getByText('LAN')).toBeInTheDocument();
    });

    it('shows no subnets message when API returns empty', async () => {
        getSubnets.mockResolvedValue({ subnets: [] });

        render(SubnetPicker, {
            value: [],
            routes: [],
            onchange: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByText(/no subnets detected/i)).toBeInTheDocument();
        });
    });

    it('shows error for invalid CIDR on add', async () => {
        getSubnets.mockResolvedValue({ subnets: [] });

        render(SubnetPicker, {
            value: [],
            routes: [],
            onchange: vi.fn(),
        });

        await waitFor(() => {
            expect(screen.getByText(/no subnets detected/i)).toBeInTheDocument();
        });

        const input = screen.getByPlaceholderText('10.0.0.0/24');
        await fireEvent.input(input, { target: { value: 'invalid-cidr' } });
        await fireEvent.click(screen.getByRole('button', { name: /add/i }));
        expect(screen.getByText(/invalid cidr format/i)).toBeInTheDocument();
    });

    it('calls onchange with valid CIDR', async () => {
        getSubnets.mockResolvedValue({ subnets: [] });
        const onchange = vi.fn();

        render(SubnetPicker, {
            value: [],
            routes: [],
            onchange,
        });

        await waitFor(() => {
            expect(screen.getByPlaceholderText('10.0.0.0/24')).toBeInTheDocument();
        });

        const input = screen.getByPlaceholderText('10.0.0.0/24');
        await fireEvent.input(input, { target: { value: '10.20.0.0/24' } });
        await fireEvent.click(screen.getByRole('button', { name: /add/i }));
        expect(onchange).toHaveBeenCalledWith(['10.20.0.0/24']);
    });
});
