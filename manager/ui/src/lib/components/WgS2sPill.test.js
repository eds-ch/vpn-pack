import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import WgS2sPill from './WgS2sPill.svelte';

describe('WgS2sPill', () => {
    it('renders nothing when no tunnels', () => {
        const { container } = render(WgS2sPill, { tunnels: [] });
        expect(container.textContent.trim()).toBe('');
    });

    it('shows count when all tunnels connected', () => {
        const tunnels = [
            { id: '1', connected: true },
            { id: '2', connected: true },
        ];
        render(WgS2sPill, { tunnels });
        expect(screen.getByText('WG S2S')).toBeInTheDocument();
        expect(screen.getByText('2/2')).toBeInTheDocument();
    });

    it('shows partial count when some tunnels disconnected', () => {
        const tunnels = [
            { id: '1', connected: true },
            { id: '2', connected: false },
            { id: '3', connected: false },
        ];
        render(WgS2sPill, { tunnels });
        expect(screen.getByText('1/3')).toBeInTheDocument();
    });

    it('shows 0/N when no tunnels connected', () => {
        const tunnels = [
            { id: '1', connected: false },
        ];
        render(WgS2sPill, { tunnels });
        expect(screen.getByText('0/1')).toBeInTheDocument();
    });
});
