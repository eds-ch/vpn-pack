<script>
    import { wgS2sUpdateTunnel, wgS2sDeleteTunnel, wgS2sEnableTunnel, wgS2sDisableTunnel } from '../api.js';
    import { formatBytes, relativeTime, validateTunnelFields, tunnelStatusInfo } from '../utils.js';
    import { WG_DEFAULT_MTU, WG_DEFAULT_KEEPALIVE, WG_DEFAULT_ROUTE_METRIC } from '../constants.js';
    import { useClipboard } from '../helpers/clipboard.svelte.js';
    import { clearFieldError } from '../helpers/field-errors.js';
    import FormField from './FormField.svelte';
    import Button from './Button.svelte';
    import WgConfigCopy from './WgConfigCopy.svelte';

    let { tunnel, onUpdate, onDelete } = $props();

    let editing = $state(false);
    let editData = $state({});
    let fieldErrors = $state({});
    let showDeleteConfirm = $state(false);
    let configVisible = $state(false);
    let actionLoading = $state(false);
    const clip = useClipboard();

    let si = $derived(tunnelStatusInfo(tunnel));

    function startEdit() {
        fieldErrors = {};
        editData = {
            name: tunnel.name,
            listenPort: tunnel.listenPort,
            tunnelAddress: tunnel.tunnelAddress,
            peerPublicKey: tunnel.peerPublicKey ?? '',
            peerEndpoint: tunnel.peerEndpoint ?? '',
            allowedIPs: (tunnel.allowedIPs ?? []).join(', '),
            localSubnets: (tunnel.localSubnets ?? []).join(', '),
            persistentKeepalive: tunnel.persistentKeepalive ?? WG_DEFAULT_KEEPALIVE,
            mtu: tunnel.mtu ?? WG_DEFAULT_MTU,
            routeMetric: tunnel.routeMetric ?? WG_DEFAULT_ROUTE_METRIC,
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
            localSubnets: editData.localSubnets.split(',').map(s => s.trim()).filter(Boolean),
            persistentKeepalive: Number(editData.persistentKeepalive),
            mtu: Number(editData.mtu),
            routeMetric: Number(editData.routeMetric),
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
                    <div>
                        <span class="text-text-secondary">Route Metric</span>
                        <span class="ml-2 text-text">{tunnel.routeMetric ?? WG_DEFAULT_ROUTE_METRIC}</span>
                    </div>
                    {#if tunnel.publicKey}
                        <div class="md:col-span-2">
                            <span class="text-text-secondary">Public Key</span>
                            <div class="mt-1 flex gap-2">
                                <input
                                    type="text"
                                    readonly
                                    value={tunnel.publicKey}
                                    class="flex-1 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text font-mono text-caption"
                                />
                                <Button variant="secondary" size="sm" onclick={() => clip.copy(tunnel.publicKey)}>{clip.copied ? 'Copied!' : clip.copyFailed ? 'Copy failed' : 'Copy'}</Button>
                            </div>
                        </div>
                    {/if}
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
                    {#if (tunnel.localSubnets ?? []).length > 0}
                        <div class="md:col-span-2">
                            <span class="text-text-secondary">Local Subnets</span>
                            <span class="ml-2 text-text font-mono text-caption break-all">
                                {tunnel.localSubnets.join(', ')}
                            </span>
                        </div>
                    {/if}
                    {#if (tunnel.allowedIPs ?? []).length > 0}
                        <div class="md:col-span-2">
                            <span class="text-text-secondary">Remote Subnets</span>
                            <span class="ml-2 text-text font-mono text-caption break-all">
                                {tunnel.allowedIPs.join(', ')}
                            </span>
                        </div>
                    {/if}
                </div>

                <div class="flex flex-wrap gap-2 pt-2">
                    <Button variant="secondary" size="sm" onclick={startEdit}>Edit</Button>
                    <Button variant="secondary" size="sm" disabled={actionLoading} onclick={handleToggle}>{tunnel.enabled !== false ? 'Disable' : 'Enable'}</Button>
                    <Button variant="secondary" size="sm" onclick={() => configVisible = !configVisible}>{configVisible ? 'Hide Config' : 'Copy Config'}</Button>
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
                            <Button variant="secondary" size="sm" onclick={() => showDeleteConfirm = false}>Cancel</Button>
                        </div>
                    </div>
                {/if}
            {:else}
                <div class="space-y-3">
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <FormField label="Tunnel Name" bind:value={editData.name}
                            error={fieldErrors.name}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'name')} />
                        <FormField label="Listen Port" type="number" bind:value={editData.listenPort}
                            error={fieldErrors.listenPort}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'listenPort')} />
                        <FormField label="Tunnel Address (CIDR)" bind:value={editData.tunnelAddress}
                            error={fieldErrors.tunnelAddress}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'tunnelAddress')} />
                        <FormField label="MTU" type="number" bind:value={editData.mtu}
                            error={fieldErrors.mtu}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'mtu')} />
                        <FormField label="Route Metric" type="number" bind:value={editData.routeMetric}
                            error={fieldErrors.routeMetric}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'routeMetric')} />
                    </div>
                    <FormField label="Peer Public Key" bind:value={editData.peerPublicKey}
                        error={fieldErrors.peerPublicKey} extraClass="font-mono text-caption"
                        oninput={() => fieldErrors = clearFieldError(fieldErrors,'peerPublicKey')} />
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <FormField label="Peer Endpoint" bind:value={editData.peerEndpoint}
                            error={fieldErrors.peerEndpoint}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'peerEndpoint')} />
                        <FormField label="Persistent Keepalive (s)" type="number" bind:value={editData.persistentKeepalive}
                            error={fieldErrors.persistentKeepalive}
                            oninput={() => fieldErrors = clearFieldError(fieldErrors,'persistentKeepalive')} />
                    </div>
                    <FormField label="Local Subnets (comma-separated)" bind:value={editData.localSubnets}
                        error={fieldErrors.localSubnets} extraClass="font-mono text-caption"
                        oninput={() => fieldErrors = clearFieldError(fieldErrors,'localSubnets')} />
                    <FormField label="Remote Subnets (comma-separated)" bind:value={editData.allowedIPs}
                        error={fieldErrors.allowedIPs} extraClass="font-mono text-caption"
                        oninput={() => fieldErrors = clearFieldError(fieldErrors,'allowedIPs')} />
                    <div class="flex gap-2 pt-1">
                        <Button variant="primary" size="sm" disabled={actionLoading} onclick={applyEdit}>{actionLoading ? 'Applying...' : 'Apply'}</Button>
                        <Button variant="secondary" size="sm" onclick={() => editing = false}>Cancel</Button>
                    </div>
                </div>
            {/if}
        </div>
</div>
