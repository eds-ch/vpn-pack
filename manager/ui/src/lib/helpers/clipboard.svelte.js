import { COPY_NOTIFICATION_MS } from '../constants.js';

export function useClipboard(timeoutMs = COPY_NOTIFICATION_MS) {
    let copied = $state(false);
    let copyFailed = $state(false);
    let timer = null;

    function clearTimer() {
        if (timer != null) {
            clearTimeout(timer);
            timer = null;
        }
    }

    async function copy(text) {
        if (!text) return;
        clearTimer();
        try {
            await navigator.clipboard.writeText(text);
            copied = true;
            copyFailed = false;
            timer = setTimeout(() => { copied = false; timer = null; }, timeoutMs);
        } catch (e) {
            console.warn('Clipboard write failed:', e);
            copied = false;
            copyFailed = true;
            timer = setTimeout(() => { copyFailed = false; timer = null; }, timeoutMs);
        }
    }

    function destroy() {
        clearTimer();
    }

    return {
        get copied() { return copied; },
        get copyFailed() { return copyFailed; },
        copy,
        destroy,
    };
}
