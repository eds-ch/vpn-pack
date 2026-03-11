<script>
    import Toggle from './Toggle.svelte';
    import Button from './Button.svelte';
    import { isValidIPOrCIDR } from '../utils.js';

    let {
        mode = 'off',
        clients = [],
        activeVPNClients = [],
        dpiFingerprinting = null,
        onchange,
    } = $props();

    let showConfirm = $state(false);
    let clientIP = $state('');
    let clientLabel = $state('');
    let clientError = $state('');

    let isOn = $derived(mode !== 'off');
    let hasWgclt = $derived(activeVPNClients.length > 0);
    let wgcltNames = $derived(activeVPNClients.join(', '));

    function handleToggle() {
        if (isOn) {
            showConfirm = false;
            onchange?.('off', []);
        } else {
            showConfirm = true;
            onchange?.('all', clients);
        }
    }

    function selectMode(newMode) {
        if (newMode === 'all' && mode !== 'all') {
            showConfirm = true;
        }
        onchange?.(newMode, clients);
    }

    function confirmEnable() {
        showConfirm = false;
    }

    function cancelConfirm() {
        showConfirm = false;
        onchange?.('off', []);
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
        onchange?.(mode, [...clients, { ip, label: clientLabel.trim() || undefined }]);
        clientIP = '';
        clientLabel = '';
    }

    function removeClient(ip) {
        onchange?.(mode, clients.filter(c => c.ip !== ip));
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') addClient();
    }
</script>

<div class="py-4">
    <div class="flex justify-between items-center">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Advertise as Exit Node</span>
            <p class="text-caption text-text-tertiary mt-0.5">Route internet traffic from other devices through Tailscale.</p>
            {#if hasWgclt && isOn}
                <p class="text-caption text-warning mt-1">
                    Advertising is safe, but don't route this device's own traffic through a remote exit node — Tailscale ip rules have higher priority and would override {wgcltNames} routing.
                </p>
            {/if}
            {#if dpiFingerprinting === false}
                <p class="text-caption text-warning mt-1">
                    DPI fingerprinting is disabled while exit node is active to prevent system instability.
                </p>
            {/if}
        </div>
        <Toggle checked={isOn} onchange={handleToggle} />
    </div>

    {#if showConfirm && mode === 'all'}
        <div class="mt-3 p-3 rounded-lg bg-warning/10 border border-warning/30">
            <p class="text-body text-warning font-bold mb-1">Confirm exit node</p>
            <p class="text-caption text-text-secondary mb-3">
                ALL internet traffic from ALL clients behind this router
                (all VLANs, all devices) will be routed through the Tailscale
                exit node. Direct internet access will be lost.
            </p>
            <div class="flex gap-2">
                <Button variant="warning" size="sm" onclick={confirmEnable}>Enable Exit Node</Button>
                <Button variant="secondary" size="sm" onclick={cancelConfirm}>Cancel</Button>
            </div>
        </div>
    {/if}

    {#if isOn && !showConfirm}
        <div class="mt-3 space-y-1">
            <span class="text-caption text-text-tertiary">Routing mode</span>
            <div class="flex gap-1">
                <button
                    onclick={() => selectMode('all')}
                    class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                        {mode === 'all'
                            ? 'bg-blue/15 text-blue border border-blue/40'
                            : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                >All traffic</button>
                <button
                    onclick={() => selectMode('selective')}
                    class="flex-1 px-3 py-1.5 rounded-lg text-caption text-center transition-colors
                        {mode === 'selective'
                            ? 'bg-blue/15 text-blue border border-blue/40'
                            : 'bg-surface text-text-secondary border border-border hover:bg-surface-hover'}"
                >Selected clients</button>
            </div>
        </div>

        {#if mode === 'selective'}
            <div class="mt-3">
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
                    <p class="text-caption text-text-tertiary mb-3">No clients added. Exit node will not route traffic until clients are specified.</p>
                {/if}

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
            </div>
        {/if}
    {/if}
</div>
