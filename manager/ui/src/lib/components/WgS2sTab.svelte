<script>
    import { onMount } from 'svelte';
    import { wgS2sListTunnels } from '../api.js';
    import TunnelCard from './TunnelCard.svelte';
    import TunnelForm from './TunnelForm.svelte';

    let { status } = $props();

    let showForm = $state(false);
    let tunnels = $state([]);
    let loading = $state(true);

    let mergedTunnels = $derived.by(() => {
        const live = status.wgS2sTunnels ?? [];
        const liveMap = new Map(live.map(t => [t.id, t]));
        return tunnels.map(t => {
            const liveData = liveMap.get(t.id);
            if (!liveData) return t;
            return {
                ...t,
                connected: liveData.connected,
                lastHandshake: liveData.lastHandshake,
                transferRx: liveData.transferRx,
                transferTx: liveData.transferTx,
                endpoint: liveData.endpoint || t.endpoint,
                forwardINOk: liveData.forwardINOk,
            };
        });
    });

    onMount(() => {
        refreshTunnels();
    });

    async function refreshTunnels() {
        const data = await wgS2sListTunnels();
        if (Array.isArray(data)) {
            tunnels = data;
        }
        loading = false;
    }

    function handleCreated(tunnel) {
        tunnels = [...tunnels, tunnel];
        showForm = false;
    }
</script>

<div class="space-y-8">
    <div class="flex justify-between items-start">
        <div>
            <h2 class="text-heading text-text-heading">S2S Tunnels</h2>
            <p class="text-caption text-text-tertiary mt-1">Direct encrypted connections between remote networks</p>
        </div>
        <button
            onclick={() => showForm = !showForm}
            class="px-4 py-2 rounded-lg text-body font-bold bg-blue text-white hover:bg-blue-hover transition-colors"
        >
            {showForm ? 'Cancel' : 'Create Tunnel'}
        </button>
    </div>

    {#if showForm}
        <TunnelForm onCreated={handleCreated} onCancel={() => showForm = false} integrationConfigured={status.integrationStatus?.configured ?? false} />
    {/if}

    {#if loading}
        <div class="space-y-4">
            {#each [1, 2] as _, i (i)}
                <div class="h-24 bg-surface rounded-xl animate-pulse"></div>
            {/each}
        </div>
    {:else if mergedTunnels.length === 0 && !showForm}
        <div class="relative rounded-xl border border-border bg-surface/50 p-8 text-center">
            <p class="text-body text-text-secondary">
                No WireGuard site-to-site tunnels configured. Click "Create Tunnel" to add one.
            </p>
        </div>
    {:else}
        <div class="space-y-3">
            {#each mergedTunnels as tunnel (tunnel.id)}
                <TunnelCard {tunnel} onUpdate={refreshTunnels} onDelete={refreshTunnels} />
            {/each}
        </div>
    {/if}
</div>
