<script>
    import { onMount } from 'svelte';
    import { wgS2sGetConfig } from '../api.js';
    import { COPY_NOTIFICATION_MS } from '../constants.js';

    let { tunnelId } = $props();

    let config = $state('');
    let loading = $state(true);
    let copied = $state(false);
    let copyFailed = $state(false);

    onMount(async () => {
        const data = await wgS2sGetConfig(tunnelId);
        if (data?.config) {
            config = data.config;
        }
        loading = false;
    });

    async function copyConfig() {
        if (!config) return;
        try {
            await navigator.clipboard.writeText(config);
            copied = true;
            copyFailed = false;
            setTimeout(() => copied = false, COPY_NOTIFICATION_MS);
        } catch (e) {
            console.warn('Clipboard write failed:', e);
            copyFailed = true;
            setTimeout(() => copyFailed = false, COPY_NOTIFICATION_MS);
        }
    }
</script>

<div class="mt-3 space-y-2">
    {#if loading}
        <div class="h-24 bg-panel rounded-lg animate-pulse"></div>
    {:else if config}
        <pre class="bg-panel rounded-lg p-3 text-caption text-text font-mono overflow-x-auto whitespace-pre border border-border">{config}</pre>
        <div class="flex items-center gap-3">
            <button
                onclick={copyConfig}
                class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
            >{copied ? 'Copied!' : copyFailed ? 'Copy failed' : 'Copy to Clipboard'}</button>
            <span class="text-caption text-text-secondary">
                If remote side also runs VPN Pack, paste this into their tunnel creation form.
            </span>
        </div>
    {:else}
        <p class="text-caption text-text-secondary">Failed to generate config</p>
    {/if}
</div>
