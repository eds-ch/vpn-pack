import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useClipboard } from './clipboard.svelte.js';

describe('useClipboard', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        Object.assign(navigator, {
            clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
        });
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('sets copied=true after successful copy', async () => {
        const clip = useClipboard(1000);
        await clip.copy('hello');
        expect(clip.copied).toBe(true);
        expect(clip.copyFailed).toBe(false);
    });

    it('resets copied after timeout', async () => {
        const clip = useClipboard(1000);
        await clip.copy('hello');
        expect(clip.copied).toBe(true);
        vi.advanceTimersByTime(1000);
        expect(clip.copied).toBe(false);
    });

    it('sets copyFailed on clipboard error', async () => {
        navigator.clipboard.writeText = vi.fn().mockRejectedValue(new Error('denied'));
        const clip = useClipboard(1000);
        await clip.copy('hello');
        expect(clip.copyFailed).toBe(true);
        expect(clip.copied).toBe(false);
    });

    it('resets copyFailed after timeout', async () => {
        navigator.clipboard.writeText = vi.fn().mockRejectedValue(new Error('denied'));
        const clip = useClipboard(1000);
        await clip.copy('hello');
        vi.advanceTimersByTime(1000);
        expect(clip.copyFailed).toBe(false);
    });

    it('does nothing when text is empty', async () => {
        const clip = useClipboard(1000);
        await clip.copy('');
        expect(clip.copied).toBe(false);
        expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
    });

    it('destroy clears pending timer', async () => {
        const clip = useClipboard(1000);
        await clip.copy('hello');
        clip.destroy();
        vi.advanceTimersByTime(1000);
        expect(clip.copied).toBe(true);
    });
});
