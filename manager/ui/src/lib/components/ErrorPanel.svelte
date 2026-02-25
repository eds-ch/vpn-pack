<script>
    import { dismissError } from '../stores/tailscale.svelte.js';

    let { errors } = $props();
</script>

{#if errors.length > 0}
    <div class="shrink-0 border-t border-border bg-panel max-h-48 overflow-y-auto">
        {#each errors as error (error.id)}
            <div class="flex items-start gap-3 px-4 py-2 border-l-2 border-error">
                <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 text-caption text-text-secondary">
                        <span>{new Date(error.timestamp).toLocaleTimeString()}</span>
                    </div>
                    <p class="text-body text-error mt-0.5">{error.message}</p>
                </div>
                <button
                    onclick={() => dismissError(error.id)}
                    class="shrink-0 w-6 h-6 flex items-center justify-center rounded text-text-secondary hover:text-text hover:bg-surface transition-colors text-caption"
                    aria-label="Dismiss error"
                >
                    &#10005;
                </button>
            </div>
        {/each}
    </div>
{/if}
