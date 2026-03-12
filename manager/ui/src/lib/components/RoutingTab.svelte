<script>
    import SubnetPicker from './SubnetPicker.svelte';
    import ExitNodeToggle from './ExitNodeToggle.svelte';
    import RemoteExitNode from './RemoteExitNode.svelte';
    import Button from './Button.svelte';
    import { setRoutes } from '../api.js';

    let { status, deviceInfo } = $props();

    let exitNode = $derived(status.exitNode);
    let routes = $derived(status.routes || []);
    let activeVPNClients = $derived(deviceInfo?.activeVPNClients || []);
    let isRunning = $derived(status.backendState === 'Running');

    let stagedCidrs = $state(null);
    let stagedAdvertiseExit = $state(null);
    let applying = $state(false);
    let userTouched = $state(false);
    let advertiseConfirmPending = $state(false);

    let initialized = $derived(stagedCidrs !== null);

    $effect(() => {
        if (!status.usingExitNode) advertiseConfirmPending = false;
    });

    $effect.pre(() => {
        if (isRunning && !userTouched) {
            stagedCidrs = routes.map(r => r.cidr);
            stagedAdvertiseExit = exitNode;
        }
    });

    $effect(() => {
        if (!isRunning) {
            userTouched = false;
            stagedCidrs = null;
            stagedAdvertiseExit = null;
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
        if (stagedAdvertiseExit !== exitNode) return true;
        return false;
    });

    async function handleApply() {
        applying = true;
        const result = await setRoutes(stagedCidrs, stagedAdvertiseExit);
        if (result?.ok) userTouched = false;
        applying = false;
    }
</script>

<div class="mb-8">
    <h2 class="text-heading text-text-heading">Routing</h2>
    <p class="text-caption text-text-tertiary mt-1">Subnet routes and exit node configuration</p>
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
            enabled={stagedAdvertiseExit}
            {activeVPNClients}
            dpiFingerprinting={status.dpiFingerprinting}
            onchange={(enabled) => {
                if (enabled && status.usingExitNode) {
                    advertiseConfirmPending = true;
                } else {
                    stagedAdvertiseExit = enabled;
                    userTouched = true;
                    advertiseConfirmPending = false;
                }
            }}
        />
        {#if advertiseConfirmPending}
            <div class="p-3 rounded-lg bg-warning/10 border border-warning/30 -mt-2 mb-2">
                <p class="text-body text-warning font-bold mb-1">Disable remote exit node?</p>
                <p class="text-caption text-text-secondary mb-3">
                    Remote exit node ({status.usingExitNode.hostName}) will be disabled.
                    Traffic will no longer be routed through it.
                </p>
                <div class="flex gap-2">
                    <Button variant="warning" size="sm" onclick={() => {
                        stagedAdvertiseExit = true;
                        userTouched = true;
                        advertiseConfirmPending = false;
                    }}>Continue</Button>
                    <Button variant="secondary" size="sm" onclick={() => {
                        advertiseConfirmPending = false;
                    }}>Cancel</Button>
                </div>
            </div>
        {/if}
    </div>

    <div class="flex justify-end mt-6">
        <Button disabled={!hasChanges || applying} onclick={handleApply}>
            {applying ? 'Applying...' : 'Apply'}
        </Button>
    </div>

    <div class="mt-8 pt-6 border-t border-border">
        <RemoteExitNode
            current={status.usingExitNode}
        />
    </div>
{/if}
