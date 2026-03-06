<script>
    import { onMount } from 'svelte';
    import { wgS2sGetConfig } from '../api.js';
    import { useClipboard } from '../helpers/clipboard.svelte.js';

    let { tunnelId } = $props();

    let config = $state('');
    let loading = $state(true);
    const clip = useClipboard();

    onMount(async () => {
        const data = await wgS2sGetConfig(tunnelId);
        if (data?.config) {
            config = data.config;
        }
        loading = false;
    });
</script>

<div class="mt-3 space-y-2">
    {#if loading}
        <div class="h-24 bg-panel rounded-lg animate-pulse"></div>
    {:else if config}
        <pre class="bg-panel rounded-lg p-3 text-caption text-text font-mono overflow-x-auto whitespace-pre border border-border">{config}</pre>
        <div class="flex items-center gap-3">
            <button
                onclick={() => clip.copy(config)}
                class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
            >{clip.copied ? 'Copied!' : clip.copyFailed ? 'Copy failed' : 'Copy to Clipboard'}</button>
        </div>
    {:else}
        <p class="text-caption text-text-secondary">Failed to generate config</p>
    {/if}
</div>
