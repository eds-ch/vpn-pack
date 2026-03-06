<script>
    import SubnetPicker from './SubnetPicker.svelte';
    import ExitNodeToggle from './ExitNodeToggle.svelte';
    import { setRoutes } from '../api.js';

    let { status, deviceInfo } = $props();

    let exitNode = $derived(status.exitNode);
    let routes = $derived(status.routes || []);
    let activeVPNClients = $derived(deviceInfo?.activeVPNClients || []);
    let isRunning = $derived(status.backendState === 'Running');

    let stagedCidrs = $state(null);
    let stagedExitNode = $state(null);
    let applying = $state(false);
    let userTouched = $state(false);

    let initialized = $derived(stagedCidrs !== null);

    $effect.pre(() => {
        if (isRunning && !userTouched) {
            stagedCidrs = routes.map(r => r.cidr);
            stagedExitNode = exitNode;
        }
    });

    $effect(() => {
        if (!isRunning) {
            userTouched = false;
            stagedCidrs = null;
            stagedExitNode = null;
        }
    });

    let hasChanges = $derived.by(() => {
        if (!initialized) return false;
        const origSet = new Set(routes.map(r => r.cidr));
        const stagedSet = new Set(stagedCidrs);
        if (origSet.size !== stagedSet.size) return true;
        for (const c of origSet) {
            if (!stagedSet.has(c)) return true;
        }
        return stagedExitNode !== exitNode;
    });

    async function handleApply() {
        applying = true;
        await setRoutes(stagedCidrs, stagedExitNode);
        userTouched = false;
        applying = false;
    }
</script>

<div class="mb-8">
    <h2 class="text-heading text-text-heading">Routing</h2>
    <p class="text-caption text-text-tertiary mt-1">Subnet routes and exit node advertising</p>
</div>

{#if !isRunning}
    <div class="relative rounded-xl border border-border bg-surface/50 p-8 text-center">
        <p class="text-body text-text-secondary">Tailscale must be connected to configure routing.</p>
    </div>
{:else if !initialized}
    <div class="space-y-4">
        {#each [1, 2, 3] as _, i (i)}
            <div class="h-14 bg-surface rounded-lg animate-pulse"></div>
        {/each}
    </div>
{:else}
    <div class="divide-y divide-border">
        <SubnetPicker
            value={stagedCidrs}
            {routes}
            onchange={(cidrs) => { stagedCidrs = cidrs; userTouched = true; }}
        />
        <ExitNodeToggle
            value={stagedExitNode}
            {activeVPNClients}
            dpiFingerprinting={status.dpiFingerprinting}
            onchange={(val) => { stagedExitNode = val; userTouched = true; }}
        />
    </div>

    <div class="flex justify-end mt-6">
        <button
            onclick={handleApply}
            disabled={!hasChanges || applying}
            class="px-6 py-2 rounded-lg text-body font-bold bg-blue text-white transition-colors
                {hasChanges && !applying ? 'hover:bg-blue-hover' : 'opacity-50 cursor-not-allowed'}"
        >
            {applying ? 'Applying...' : 'Apply'}
        </button>
    </div>
{/if}
