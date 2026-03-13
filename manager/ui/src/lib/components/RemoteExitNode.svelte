<script>
    import Toggle from './Toggle.svelte';
    import { isValidIPOrCIDR } from '../utils.js';

    let {
        peers = [],
        peersLoading = false,
        current = null,
        enabled = false,
        selectedPeerId = '',
        mode = 'all',
        clients = [],
        ontoggle,
        onpeerchange,
        onmodechange,
        onclientschange,
    } = $props();

    const MAX_EXIT_CLIENTS = 20;

    let clientIP = $state('');
    let clientLabel = $state('');
    let clientError = $state('');

    let atClientLimit = $derived(clients.length >= MAX_EXIT_CLIENTS);
    let onlinePeers = $derived(peers.filter(p => p.online));
    let offlinePeers = $derived(peers.filter(p => !p.online));
    let selectedPeer = $derived(peers.find(p => p.id === selectedPeerId));

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
        onclientschange?.([...clients, { ip, label: clientLabel.trim() || undefined }]);
        clientIP = '';
        clientLabel = '';
    }

    function removeClient(ip) {
        onclientschange?.(clients.filter(c => c.ip !== ip));
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') addClient();
    }
</script>

<div class="py-4">
    <div class="flex justify-between items-center">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Use Remote Exit Node</span>
            <p class="text-caption text-text-tertiary mt-0.5">Route LAN clients' internet traffic through another Tailscale device.</p>
        </div>
        <Toggle checked={enabled} onchange={(e) => ontoggle?.(e.target.checked)} />
    </div>

    {#if enabled}
        <div class="mt-3 space-y-4">
            {#if peersLoading}
                <div class="h-10 bg-surface rounded-lg animate-pulse"></div>
            {:else if peers.length === 0}
                <p class="text-caption text-text-tertiary">No exit node peers available in your tailnet.</p>
            {:else}
                <div>
                    <label class="block text-caption text-text-tertiary mb-1.5" for="exit-peer-select">Peer</label>
                    <div class="relative flex items-center">
                        {#if selectedPeer}
                            <span class="absolute left-3 z-10 w-2 h-2 rounded-full {selectedPeer.online ? 'bg-success' : 'bg-error'}"></span>
                        {/if}
                        <select
                            id="exit-peer-select"
                            value={selectedPeerId}
                            onchange={(e) => onpeerchange?.(e.target.value)}
                            class="w-full py-2 pr-3 text-body rounded-lg border border-border bg-input text-text focus:outline-none focus:border-blue appearance-none {selectedPeer ? 'pl-8' : 'pl-3'}"
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
                </div>

                {#if current && !current.online}
                    <p class="text-caption text-warning">Exit node is offline. Traffic will not be routed until it comes back online.</p>
                {/if}

                {#if selectedPeerId}
                    <div>
                        <span class="block text-caption text-text-tertiary mb-1.5">Routing mode</span>
                        <div class="flex gap-1">
                            <button
                                onclick={() => onmodechange?.('all')}
                                class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                                    {mode === 'all'
                                        ? 'bg-blue/15 text-blue border border-blue/40'
                                        : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                            >All traffic</button>
                            <button
                                onclick={() => onmodechange?.('selective')}
                                class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                                    {mode === 'selective'
                                        ? 'bg-blue/15 text-blue border border-blue/40'
                                        : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                            >Selected clients</button>
                        </div>
                    </div>

                    {#if mode === 'selective'}
                        <div class="space-y-3">
                            {#if clients.length > 0}
                                <div class="flex flex-wrap gap-1.5">
                                    {#each clients as client (client.ip)}
                                        <span class="inline-flex items-center rounded border border-blue/20 overflow-hidden text-caption">
                                            <button
                                                onclick={() => removeClient(client.ip)}
                                                class="flex items-center px-1.5 self-stretch border-r border-blue/20 text-error/50 hover:text-error hover:bg-error/10 transition-colors"
                                                aria-label="Remove {client.ip}"
                                            >&times;</button>
                                            <span class="px-2 py-0.5 font-mono text-text bg-blue/10">{client.ip}</span>
                                            {#if client.label}
                                                <span class="px-2 py-0.5 text-text-tertiary border-l border-blue/20">{client.label}</span>
                                            {/if}
                                        </span>
                                    {/each}
                                </div>
                            {:else}
                                <p class="text-caption text-text-tertiary">No clients added. Only specified clients will be routed through the exit node. Clients must have a static IP assigned in UniFi Network settings.</p>
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
                {/if}
            {/if}
        </div>
    {/if}
</div>
