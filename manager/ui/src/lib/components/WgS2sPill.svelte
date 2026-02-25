<script>
    let { tunnels = [] } = $props();

    let total = $derived(tunnels.length);
    let up = $derived(tunnels.filter(t => t.connected).length);

    let dotColor = $derived(
        total === 0 ? 'bg-text-secondary' :
        up === total ? 'bg-success' :
        up > 0 ? 'bg-warning' : 'bg-error'
    );
</script>

{#if total > 0}
    <div class="flex items-center gap-1.5 text-caption">
        <span class="w-2 h-2 rounded-full {dotColor} shrink-0"></span>
        <span class="text-text">WG S2S</span>
        <span class="text-text-tertiary">·</span>
        <span class="text-text-secondary">{up}/{total}</span>
    </div>
{/if}
