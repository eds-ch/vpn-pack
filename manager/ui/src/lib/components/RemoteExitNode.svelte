<script>
    import Button from './Button.svelte';
    import { isValidIPOrCIDR } from '../utils.js';
    import { getRemoteExitNode, enableRemoteExitNode, disableRemoteExitNode } from '../api.js';

    let {
        current = null,
        advertiseEnabled = false,
    } = $props();

    const MAX_EXIT_CLIENTS = 20;

    let peers = $state([]);
    let loading = $state(false);
    let disabling = $state(false);
    let enabling = $state(false);

    let selectedPeerId = $state('');
    let mode = $state('all');
    let clients = $state([]);
    let clientIP = $state('');
    let clientLabel = $state('');
    let clientError = $state('');

    let confirmWarning = $state('');
    let awaitingConfirm = $state(false);

    let isActive = $derived(current != null);
let atClientLimit = $derived(clients.length >= MAX_EXIT_CLIENTS);
    let onlinePeers = $derived(peers.filter(p => p.online));
    let offlinePeers = $derived(peers.filter(p => !p.online));

    $effect(() => {
        fetchPeers();
    });

    async function fetchPeers() {
        loading = true;
        const resp = await getRemoteExitNode();
        loading = false;
        if (resp) {
            peers = resp.peers ?? [];
        }
    }

    function resetForm() {
        selectedPeerId = '';
        mode = 'all';
        clients = [];
        awaitingConfirm = false;
        confirmWarning = '';
    }

    async function handleEnable() {
        if (!selectedPeerId) return;

        enabling = true;

        const result = await enableRemoteExitNode({
            peerId: selectedPeerId,
            mode,
            clients: mode === 'selective' ? clients : undefined,
            confirm: awaitingConfirm,
        });

        enabling = false;

        if (!result) return;

        if (result.confirmRequired) {
            confirmWarning = result.message;
            awaitingConfirm = true;
            return;
        }

        if (result.ok) {
            resetForm();
            await fetchPeers();
        }
    }

    async function handleDisable() {
        disabling = true;
        await disableRemoteExitNode();
        disabling = false;
        resetForm();
        await fetchPeers();
    }

    function cancelConfirm() {
        awaitingConfirm = false;
        confirmWarning = '';
    }

    function addClient() {
        const ip = clientIP.trim();
        if (!ip) return;
        if (!isValidIPOrCIDR(ip)) {
            clientError = 'Invalid IP or CIDR (e.g. 192.168.1.100 or 10.0.0.0/24)';
            return;
        }
        if (clients.some(c => c.ip === ip)) {
            clientError = 'Client already in list';
            return;
        }
        clientError = '';
        clients = [...clients, { ip, label: clientLabel.trim() || undefined }];
        clientIP = '';
        clientLabel = '';
    }

    function removeClient(ip) {
        clients = clients.filter(c => c.ip !== ip);
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') addClient();
    }
</script>

<div class="py-6">
    <div class="mb-4">
        <h3 class="text-body text-text font-bold">Use Remote Exit Node</h3>
        <p class="text-caption text-text-tertiary mt-0.5">Route this router's internet traffic through another Tailscale device.</p>
    </div>

    {#if advertiseEnabled && isActive}
        <div class="mb-4 p-3 rounded-lg bg-warning/10 border border-warning/30">
            <p class="text-caption text-warning">
                This router is also advertising as an exit node. Using a remote exit node while advertising may create a routing loop.
            </p>
        </div>
    {/if}

    {#if isActive}
        <div class="p-4 rounded-xl border border-border bg-surface/50">
            <div class="flex items-center justify-between">
                <div class="flex items-center gap-3">
                    <span class="inline-block w-2 h-2 rounded-full {current.online ? 'bg-success' : 'bg-error'}"></span>
                    <div>
                        <span class="text-body text-text font-medium">{current.hostName}</span>
                        <span class="text-caption text-text-tertiary ml-2">{current.mode === 'all' ? 'All traffic' : 'Selected clients'}</span>
                    </div>
                </div>
                <Button variant="secondary" size="sm" disabled={disabling} onclick={handleDisable}>
                    {disabling ? 'Disabling...' : 'Disable'}
                </Button>
            </div>
            {#if !current.online}
                <p class="text-caption text-warning mt-2">Exit node is offline. Traffic will not be routed until it comes back online.</p>
            {/if}
        </div>
    {:else}
        {#if loading}
            <div class="h-10 bg-surface rounded-lg animate-pulse"></div>
        {:else if peers.length === 0}
            <p class="text-caption text-text-tertiary">No exit node peers available in your tailnet.</p>
        {:else}
            <div class="space-y-4">
                <div>
                    <label class="block text-caption text-text-tertiary mb-1.5" for="exit-peer-select">Peer</label>
                    <select
                        id="exit-peer-select"
                        bind:value={selectedPeerId}
                        onchange={() => { awaitingConfirm = false; confirmWarning = ''; }}
                        class="w-full px-3 py-2 text-body rounded-lg border border-border bg-input text-text focus:outline-none focus:border-blue appearance-none"
                    >
                        <option value="">Select a peer...</option>
                        {#if onlinePeers.length > 0}
                            <optgroup label="Online">
                                {#each onlinePeers as peer (peer.id)}
                                    <option value={peer.id}>{peer.hostName} ({peer.os})</option>
                                {/each}
                            </optgroup>
                        {/if}
                        {#if offlinePeers.length > 0}
                            <optgroup label="Offline">
                                {#each offlinePeers as peer (peer.id)}
                                    <option value={peer.id}>{peer.hostName} ({peer.os})</option>
                                {/each}
                            </optgroup>
                        {/if}
                    </select>
                </div>

                {#if selectedPeerId}
                    <div>
                        <span class="block text-caption text-text-tertiary mb-1.5">Routing mode</span>
                        <div class="flex gap-1">
                            <button
                                onclick={() => { mode = 'all'; awaitingConfirm = false; confirmWarning = ''; }}
                                class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                                    {mode === 'all'
                                        ? 'bg-blue/15 text-blue border border-blue/40'
                                        : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                            >All traffic</button>
                            <button
                                onclick={() => { mode = 'selective'; awaitingConfirm = false; confirmWarning = ''; }}
                                class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                                    {mode === 'selective'
                                        ? 'bg-blue/15 text-blue border border-blue/40'
                                        : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                            >Selected clients</button>
                        </div>
                    </div>

                    {#if mode === 'selective'}
                        <div>
                            {#if clients.length > 0}
                                <div class="space-y-0.5 mb-3">
                                    {#each clients as client (client.ip)}
                                        <div class="flex items-center gap-2.5 px-2 py-1.5 -mx-2 rounded-lg group hover:bg-surface-hover transition-colors">
                                            <span class="text-text font-mono text-caption">{client.ip}</span>
                                            {#if client.label}
                                                <span class="text-text-tertiary text-caption">{client.label}</span>
                                            {/if}
                                            <button
                                                onclick={() => removeClient(client.ip)}
                                                class="ml-auto text-text-tertiary hover:text-error text-caption transition-colors opacity-0 group-hover:opacity-100"
                                            >&times;</button>
                                        </div>
                                    {/each}
                                </div>
                            {:else}
                                <p class="text-caption text-text-tertiary mb-3">No clients added. Only specified clients will be routed through the exit node.</p>
                            {/if}

                            {#if atClientLimit}
                                <p class="text-caption text-text-tertiary">Maximum {MAX_EXIT_CLIENTS} clients reached.</p>
                            {:else}
                                <div class="flex gap-2">
                                    <input
                                        type="text"
                                        bind:value={clientIP}
                                        onkeydown={handleKeydown}
                                        placeholder="192.168.1.100 or 10.0.0.0/24"
                                        class="flex-1 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue"
                                    />
                                    <input
                                        type="text"
                                        bind:value={clientLabel}
                                        onkeydown={handleKeydown}
                                        placeholder="Label (optional)"
                                        class="w-32 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue"
                                    />
                                    <button
                                        onclick={addClient}
                                        class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
                                    >Add</button>
                                </div>
                                {#if clientError}
                                    <p class="text-caption text-error mt-1.5">{clientError}</p>
                                {/if}
                            {/if}
                        </div>
                    {/if}

                    {#if awaitingConfirm}
                        <div class="p-3 rounded-lg bg-warning/10 border border-warning/30">
                            <p class="text-body text-warning font-bold mb-1">Confirm exit node</p>
                            <p class="text-caption text-text-secondary mb-3">{confirmWarning}</p>
                            <div class="flex gap-2">
                                <Button variant="warning" size="sm" disabled={enabling} onclick={handleEnable}>
                                    {enabling ? 'Enabling...' : 'Confirm'}
                                </Button>
                                <Button variant="secondary" size="sm" onclick={cancelConfirm}>Cancel</Button>
                            </div>
                        </div>
                    {:else}
                        <Button disabled={enabling || (mode === 'selective' && clients.length === 0)} onclick={handleEnable}>
                            {enabling ? 'Enabling...' : 'Enable'}
                        </Button>
                    {/if}
                {/if}
            </div>
        {/if}
    {/if}
</div>
