import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import AuthKeyInput from './AuthKeyInput.svelte';

vi.mock('../api.js', () => ({
    connectWithAuthKey: vi.fn(),
}));

import { connectWithAuthKey } from '../api.js';

describe('AuthKeyInput', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders input and button', () => {
        render(AuthKeyInput);
        expect(screen.getByPlaceholderText('tskey-auth-...')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /connect/i })).toBeInTheDocument();
    });

    it('shows error for invalid key without tskey- prefix', async () => {
        render(AuthKeyInput);
        const input = screen.getByPlaceholderText('tskey-auth-...');
        await fireEvent.input(input, { target: { value: 'bad-key-123' } });
        await fireEvent.click(screen.getByRole('button', { name: /connect/i }));
        expect(screen.getByText(/must start with "tskey-"/i)).toBeInTheDocument();
    });

    it('calls connectWithAuthKey for valid key', async () => {
        connectWithAuthKey.mockResolvedValue({ ok: true });
        render(AuthKeyInput);
        const input = screen.getByPlaceholderText('tskey-auth-...');
        await fireEvent.input(input, { target: { value: 'tskey-auth-abc123' } });
        await fireEvent.click(screen.getByRole('button', { name: /connect/i }));
        expect(connectWithAuthKey).toHaveBeenCalledWith('tskey-auth-abc123');
    });

    it('does not call API when input is empty', async () => {
        render(AuthKeyInput);
        const button = screen.getByRole('button', { name: /connect/i });
        expect(button).toBeDisabled();
    });

    it('calls connectWithAuthKey when Enter is pressed with valid key', async () => {
        connectWithAuthKey.mockResolvedValue({ ok: true });
        render(AuthKeyInput);
        const input = screen.getByPlaceholderText('tskey-auth-...');
        await fireEvent.input(input, { target: { value: 'tskey-auth-enter-test' } });
        await fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => {
            expect(connectWithAuthKey).toHaveBeenCalledWith('tskey-auth-enter-test');
        });
    });
});
