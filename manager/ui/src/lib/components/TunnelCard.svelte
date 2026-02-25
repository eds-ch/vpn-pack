<script>
    import { wgS2sUpdateTunnel, wgS2sDeleteTunnel, wgS2sEnableTunnel, wgS2sDisableTunnel } from '../api.js';
    import { formatBytes, relativeTime, validateTunnelFields, tunnelStatusInfo } from '../utils.js';
    import { WG_DEFAULT_MTU, WG_DEFAULT_KEEPALIVE } from '../constants.js';
    import WgConfigCopy from './WgConfigCopy.svelte';

    let { tunnel, onUpdate, onDelete } = $props();

    let editing = $state(false);
    let editData = $state({});
    let fieldErrors = $state({});
    let showDeleteConfirm = $state(false);
    let configVisible = $state(false);
    let actionLoading = $state(false);

    let si = $derived(tunnelStatusInfo(tunnel));

    function inputClass(field) {
        const base = 'mt-1 w-full px-3 py-1.5 text-body rounded-lg bg-input text-text placeholder-text-secondary focus:outline-none transition-colors';
        return fieldErrors[field]
            ? `${base} border-2 border-error focus:border-error`
            : `${base} border border-border focus:border-blue`;
    }

    function clearError(field) {
        if (fieldErrors[field]) {
            fieldErrors = { ...fieldErrors };
            delete fieldErrors[field];
        }
    }

    function startEdit() {
        fieldErrors = {};
        editData = {
            name: tunnel.name,
            listenPort: tunnel.listenPort,
            tunnelAddress: tunnel.tunnelAddress,
            peerPublicKey: tunnel.peerPublicKey ?? '',
            peerEndpoint: tunnel.peerEndpoint ?? '',
            allowedIPs: (tunnel.allowedIPs ?? []).join(', '),
            persistentKeepalive: tunnel.persistentKeepalive ?? WG_DEFAULT_KEEPALIVE,
            mtu: tunnel.mtu ?? WG_DEFAULT_MTU,
        };
        editing = true;
    }

    async function applyEdit() {
        fieldErrors = validateTunnelFields(editData);
        if (Object.keys(fieldErrors).length > 0) return;

        actionLoading = true;
        const updates = {
            name: editData.name,
            listenPort: Number(editData.listenPort),
            tunnelAddress: editData.tunnelAddress,
            peerPublicKey: editData.peerPublicKey,
            peerEndpoint: editData.peerEndpoint,
            allowedIPs: editData.allowedIPs.split(',').map(s => s.trim()).filter(Boolean),
            persistentKeepalive: Number(editData.persistentKeepalive),
            mtu: Number(editData.mtu),
        };
        const result = await wgS2sUpdateTunnel(tunnel.id, updates);
        if (result) {
            editing = false;
            onUpdate();
        }
        actionLoading = false;
    }

    async function handleToggle() {
        actionLoading = true;
        const result = tunnel.enabled !== false
            ? await wgS2sDisableTunnel(tunnel.id)
            : await wgS2sEnableTunnel(tunnel.id);
        if (result) onUpdate();
        actionLoading = false;
    }

    async function confirmDelete() {
        actionLoading = true;
        const result = await wgS2sDeleteTunnel(tunnel.id);
        if (result) {
            showDeleteConfirm = false;
            onDelete();
        }
        actionLoading = false;
    }
</script>

<div class="bg-surface rounded-xl border border-border overflow-hidden">
    <div class="px-4 py-3 flex items-center gap-3">
        <span class="w-2.5 h-2.5 rounded-full shrink-0 {si.dot}"></span>
        <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
                <span class="text-body text-text truncate">{tunnel.name}</span>
                {#if tunnel.enabled === false}
                    <span class="text-caption px-1.5 py-0.5 rounded bg-warning/15 text-warning">Disabled</span>
                {/if}
            </div>
            <div class="flex flex-wrap items-center gap-x-4 gap-y-0.5 text-caption text-text-secondary mt-0.5">
                <span>{tunnel.peerEndpoint ?? ''}</span>
                <span>Handshake: {relativeTime(tunnel.lastHandshake)}</span>
                <span>TX {formatBytes(tunnel.transferTx)}</span>
                <span>RX {formatBytes(tunnel.transferRx)}</span>
            </div>
        </div>
        <span class="text-caption text-text-secondary whitespace-nowrap">{si.label}</span>
    </div>

    <div class="border-t border-border px-4 py-3 space-y-3">
            {#if !editing}
                <div class="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-2 text-body">
                    <div>
                        <span class="text-text-secondary">Interface</span>
                        <span class="ml-2 text-text font-mono">{tunnel.interfaceName ?? ''}</span>
                    </div>
                    {#if tunnel.zoneName}
                        <div>
                            <span class="text-text-secondary">Firewall Zone</span>
                            <span class="ml-2 text-text">{tunnel.zoneName}</span>
                        </div>
                    {/if}
                    <div>
                        <span class="text-text-secondary">Listen Port</span>
                        <span class="ml-2 text-text">{tunnel.listenPort ?? ''}</span>
                    </div>
                    <div>
                        <span class="text-text-secondary">Tunnel Address</span>
                        <span class="ml-2 text-text font-mono">{tunnel.tunnelAddress ?? ''}</span>
                    </div>
                    <div>
                        <span class="text-text-secondary">MTU</span>
                        <span class="ml-2 text-text">{tunnel.mtu ?? WG_DEFAULT_MTU}</span>
                    </div>
                    <div class="md:col-span-2">
                        <span class="text-text-secondary">Peer Public Key</span>
                        <span class="ml-2 text-text font-mono text-caption break-all">{tunnel.peerPublicKey ?? ''}</span>
                    </div>
                    <div>
                        <span class="text-text-secondary">Peer Endpoint</span>
                        <span class="ml-2 text-text font-mono">{tunnel.peerEndpoint ?? ''}</span>
                    </div>
                    <div>
                        <span class="text-text-secondary">Keepalive</span>
                        <span class="ml-2 text-text">{tunnel.persistentKeepalive ?? WG_DEFAULT_KEEPALIVE}s</span>
                    </div>
                    <div class="md:col-span-2">
                        <span class="text-text-secondary">Allowed IPs</span>
                        <span class="ml-2 text-text font-mono text-caption break-all">
                            {(tunnel.allowedIPs ?? []).join(', ')}
                        </span>
                    </div>
                </div>

                <div class="flex flex-wrap gap-2 pt-2">
                    <button
                        onclick={startEdit}
                        class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
                    >Edit</button>
                    <button
                        onclick={handleToggle}
                        disabled={actionLoading}
                        class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover disabled:opacity-50 transition-colors"
                    >{tunnel.enabled !== false ? 'Disable' : 'Enable'}</button>
                    <button
                        onclick={() => configVisible = !configVisible}
                        class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
                    >{configVisible ? 'Hide Config' : 'Copy Config'}</button>
                    <button
                        onclick={() => showDeleteConfirm = true}
                        class="px-3 py-1.5 text-body rounded-lg border border-error text-error hover:bg-error/10 transition-colors"
                    >Delete</button>
                </div>

                {#if configVisible}
                    <WgConfigCopy tunnelId={tunnel.id} />
                {/if}

                {#if showDeleteConfirm}
                    <div class="p-3 rounded-lg bg-error/10 border border-error/30">
                        <p class="text-body text-error mb-2">Are you sure? This will remove the tunnel and its keys.</p>
                        <div class="flex gap-2">
                            <button
                                onclick={confirmDelete}
                                disabled={actionLoading}
                                class="px-3 py-1.5 text-body rounded-lg bg-error text-white hover:bg-error/80 disabled:opacity-50 transition-colors"
                            >Delete</button>
                            <button
                                onclick={() => showDeleteConfirm = false}
                                class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
                            >Cancel</button>
                        </div>
                    </div>
                {/if}
            {:else}
                <div class="space-y-3">
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <div>
                            <span class="text-caption text-text-secondary">Tunnel Name</span>
                            <input type="text" bind:value={editData.name}
                                oninput={() => clearError('name')}
                                class={inputClass('name')} />
                            {#if fieldErrors.name}<p class="text-caption text-error mt-0.5">{fieldErrors.name}</p>{/if}
                        </div>
                        <div>
                            <span class="text-caption text-text-secondary">Listen Port</span>
                            <input type="number" bind:value={editData.listenPort}
                                oninput={() => clearError('listenPort')}
                                class={inputClass('listenPort')} />
                            {#if fieldErrors.listenPort}<p class="text-caption text-error mt-0.5">{fieldErrors.listenPort}</p>{/if}
                        </div>
                        <div>
                            <span class="text-caption text-text-secondary">Tunnel Address (CIDR)</span>
                            <input type="text" bind:value={editData.tunnelAddress}
                                oninput={() => clearError('tunnelAddress')}
                                class={inputClass('tunnelAddress')} />
                            {#if fieldErrors.tunnelAddress}<p class="text-caption text-error mt-0.5">{fieldErrors.tunnelAddress}</p>{/if}
                        </div>
                        <div>
                            <span class="text-caption text-text-secondary">MTU</span>
                            <input type="number" bind:value={editData.mtu}
                                oninput={() => clearError('mtu')}
                                class={inputClass('mtu')} />
                            {#if fieldErrors.mtu}<p class="text-caption text-error mt-0.5">{fieldErrors.mtu}</p>{/if}
                        </div>
                    </div>
                    <div>
                        <span class="text-caption text-text-secondary">Peer Public Key</span>
                        <input type="text" bind:value={editData.peerPublicKey}
                            oninput={() => clearError('peerPublicKey')}
                            class="{inputClass('peerPublicKey')} font-mono text-caption" />
                        {#if fieldErrors.peerPublicKey}<p class="text-caption text-error mt-0.5">{fieldErrors.peerPublicKey}</p>{/if}
                    </div>
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <div>
                            <span class="text-caption text-text-secondary">Peer Endpoint</span>
                            <input type="text" bind:value={editData.peerEndpoint}
                                oninput={() => clearError('peerEndpoint')}
                                class={inputClass('peerEndpoint')} />
                            {#if fieldErrors.peerEndpoint}<p class="text-caption text-error mt-0.5">{fieldErrors.peerEndpoint}</p>{/if}
                        </div>
                        <div>
                            <span class="text-caption text-text-secondary">Persistent Keepalive (s)</span>
                            <input type="number" bind:value={editData.persistentKeepalive}
                                oninput={() => clearError('persistentKeepalive')}
                                class={inputClass('persistentKeepalive')} />
                            {#if fieldErrors.persistentKeepalive}<p class="text-caption text-error mt-0.5">{fieldErrors.persistentKeepalive}</p>{/if}
                        </div>
                    </div>
                    <div>
                        <span class="text-caption text-text-secondary">Allowed IPs (comma-separated)</span>
                        <input type="text" bind:value={editData.allowedIPs}
                            oninput={() => clearError('allowedIPs')}
                            class="{inputClass('allowedIPs')} font-mono text-caption" />
                        {#if fieldErrors.allowedIPs}<p class="text-caption text-error mt-0.5">{fieldErrors.allowedIPs}</p>{/if}
                    </div>
                    <div class="flex gap-2 pt-1">
                        <button
                            onclick={applyEdit}
                            disabled={actionLoading}
                            class="px-4 py-1.5 text-body rounded-lg font-bold bg-blue text-white hover:bg-blue-hover disabled:opacity-50 transition-colors"
                        >{actionLoading ? 'Applying...' : 'Apply'}</button>
                        <button
                            onclick={() => editing = false}
                            class="px-4 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
                        >Cancel</button>
                    </div>
                </div>
            {/if}
        </div>
</div>
