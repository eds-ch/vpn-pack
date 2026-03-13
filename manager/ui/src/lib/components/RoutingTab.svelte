<script>
    import SubnetPicker from './SubnetPicker.svelte';
    import ExitNodeToggle from './ExitNodeToggle.svelte';
    import RemoteExitNode from './RemoteExitNode.svelte';
    import Button from './Button.svelte';
    import { setRoutes, getRemoteExitNode, enableRemoteExitNode, disableRemoteExitNode } from '../api.js';
    import { getStatus } from '../stores/tailscale.svelte.js';

    let { status, deviceInfo } = $props();

    let exitNode = $derived(status.exitNode);
    let routes = $derived(status.routes || []);
    let activeVPNClients = $derived(deviceInfo?.activeVPNClients || []);
    let isRunning = $derived(status.backendState === 'Running');

    let stagedCidrs = $state(null);
    let stagedAdvertiseExit = $state(null);
    let stagedRemoteExitEnabled = $state(null);
    let stagedRemoteExitPeerId = $state('');
    let stagedRemoteExitMode = $state('all');
    let stagedRemoteExitClients = $state([]);

    let applying = $state(false);
    let userTouched = $state(false);
    let advertiseConfirmPending = $state(false);
    let confirmWarning = $state('');
    let awaitingConfirm = $state(false);

    let peers = $state([]);
    let peersLoading = $state(false);

    let initialized = $derived(stagedCidrs !== null);

    $effect(() => {
        if (!status.usingExitNode) advertiseConfirmPending = false;
    });

    let peersFetched = false;

    $effect(() => {
        if (isRunning && !peersFetched) {
            peersFetched = true;
            fetchPeers();
        }
        if (!isRunning) peersFetched = false;
    });

    $effect.pre(() => {
        if (isRunning && !userTouched) {
            stagedCidrs = routes.map(r => r.cidr);
            stagedAdvertiseExit = exitNode;
            const rem = status.usingExitNode;
            stagedRemoteExitEnabled = rem != null;
            stagedRemoteExitPeerId = rem?.peerId ?? '';
            stagedRemoteExitMode = rem?.mode ?? 'all';
            stagedRemoteExitClients = rem?.clients ?? [];
        }
    });

    $effect(() => {
        if (!isRunning) {
            userTouched = false;
            stagedCidrs = null;
            stagedAdvertiseExit = null;
            stagedRemoteExitEnabled = null;
            stagedRemoteExitPeerId = '';
            stagedRemoteExitMode = 'all';
            stagedRemoteExitClients = [];
            awaitingConfirm = false;
            confirmWarning = '';
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
        const origRemoteEnabled = status.usingExitNode != null;
        if (stagedRemoteExitEnabled !== origRemoteEnabled) return true;
        if (stagedRemoteExitEnabled && origRemoteEnabled) {
            if (stagedRemoteExitPeerId !== status.usingExitNode.peerId) return true;
            if (stagedRemoteExitMode !== status.usingExitNode.mode) return true;
            if (stagedRemoteExitMode === 'selective' && !clientsEqual(stagedRemoteExitClients, status.usingExitNode.clients ?? [])) return true;
        }
        return false;
    });

    let canApply = $derived.by(() => {
        if (applying) return false;
        if (awaitingConfirm) return true;
        if (!hasChanges) return false;
        if (stagedRemoteExitEnabled && !stagedRemoteExitPeerId) return false;
        if (stagedRemoteExitEnabled && stagedRemoteExitMode === 'selective' && stagedRemoteExitClients.length === 0) return false;
        return true;
    });

    function clientsEqual(a, b) {
        if (a.length !== b.length) return false;
        return a.every((c, i) => c.ip === b[i]?.ip && (c.label ?? '') === (b[i]?.label ?? ''));
    }

    async function fetchPeers() {
        peersLoading = true;
        const resp = await getRemoteExitNode();
        peersLoading = false;
        if (resp) peers = resp.peers ?? [];
    }

    function handleRemoteExitToggle(enabled) {
        stagedRemoteExitEnabled = enabled;
        if (enabled) {
            stagedAdvertiseExit = false;
            awaitingConfirm = false;
            confirmWarning = '';
        }
        userTouched = true;
    }

    async function applyRemoteExit() {
        const wasEnabled = status.usingExitNode != null;
        const needEnable = stagedRemoteExitEnabled && (!wasEnabled
            || stagedRemoteExitPeerId !== status.usingExitNode?.peerId
            || stagedRemoteExitMode !== status.usingExitNode?.mode
            || (stagedRemoteExitMode === 'selective' && !clientsEqual(stagedRemoteExitClients, status.usingExitNode?.clients ?? [])));
        // setRoutes с advertiseExit=true сам отключает remote exit на бэкенде
        const needDisable = !stagedRemoteExitEnabled && wasEnabled && !stagedAdvertiseExit;

        if (needEnable) {
            const result = await enableRemoteExitNode({
                peerId: stagedRemoteExitPeerId,
                mode: stagedRemoteExitMode,
                clients: stagedRemoteExitMode === 'selective' ? stagedRemoteExitClients : undefined,
                confirm: awaitingConfirm,
            });
            if (!result) return false;
            if (result.confirmRequired) {
                confirmWarning = result.message;
                awaitingConfirm = true;
                return false;
            }
            if (result.ok) {
                const peer = peers.find(p => p.id === stagedRemoteExitPeerId);
                if (peer) {
                    getStatus().usingExitNode = {
                        peerId: stagedRemoteExitPeerId,
                        hostName: peer.hostName,
                        online: peer.online,
                        mode: stagedRemoteExitMode,
                        clients: stagedRemoteExitMode === 'selective' ? stagedRemoteExitClients : undefined,
                    };
                }
            }
        } else if (needDisable) {
            await disableRemoteExitNode();
            getStatus().usingExitNode = null;
        }
        return true;
    }

    async function handleApply() {
        applying = true;

        const routesResult = await setRoutes(stagedCidrs, stagedAdvertiseExit);
        if (!routesResult?.ok) { applying = false; return; }

        if (!await applyRemoteExit()) { applying = false; return; }

        userTouched = false;
        awaitingConfirm = false;
        confirmWarning = '';
        applying = false;
        await fetchPeers();
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
                if (enabled && stagedRemoteExitEnabled) {
                    advertiseConfirmPending = true;
                } else {
                    stagedAdvertiseExit = enabled;
                    if (enabled) stagedRemoteExitEnabled = false;
                    userTouched = true;
                    advertiseConfirmPending = false;
                }
            }}
        />
        {#if advertiseConfirmPending}
            <div class="p-3 rounded-lg bg-warning/10 border border-warning/30 -mt-2 mb-2">
                <p class="text-body text-warning font-bold mb-1">Disable remote exit node?</p>
                <p class="text-caption text-text-secondary mb-3">
                    Remote exit node ({status.usingExitNode?.hostName}) will be disabled.
                    Traffic will no longer be routed through it.
                </p>
                <div class="flex gap-2">
                    <Button variant="warning" size="sm" onclick={() => {
                        stagedAdvertiseExit = true;
                        stagedRemoteExitEnabled = false;
                        userTouched = true;
                        advertiseConfirmPending = false;
                    }}>Continue</Button>
                    <Button variant="secondary" size="sm" onclick={() => {
                        advertiseConfirmPending = false;
                    }}>Cancel</Button>
                </div>
            </div>
        {/if}
        <RemoteExitNode
            {peers}
            {peersLoading}
            current={status.usingExitNode}
            enabled={stagedRemoteExitEnabled}
            selectedPeerId={stagedRemoteExitPeerId}
            mode={stagedRemoteExitMode}
            clients={stagedRemoteExitClients}
            ontoggle={handleRemoteExitToggle}
            onpeerchange={(id) => { stagedRemoteExitPeerId = id; userTouched = true; awaitingConfirm = false; confirmWarning = ''; }}
            onmodechange={(m) => { stagedRemoteExitMode = m; userTouched = true; awaitingConfirm = false; confirmWarning = ''; }}
            onclientschange={(c) => { stagedRemoteExitClients = c; userTouched = true; }}
        />
    </div>

    {#if awaitingConfirm}
        <div class="p-3 rounded-lg bg-warning/10 border border-warning/30 mt-4">
            <p class="text-body text-warning font-bold mb-1">Confirm exit node change</p>
            <p class="text-caption text-text-secondary">{confirmWarning}</p>
        </div>
    {/if}

    <div class="flex justify-end mt-6">
        <Button disabled={!canApply} onclick={handleApply}>
            {applying ? 'Applying...' : awaitingConfirm ? 'Confirm & Apply' : 'Apply'}
        </Button>
    </div>
{/if}
