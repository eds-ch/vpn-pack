<script>
    import Icon from './Icon.svelte';
    import { formatBytes, relativeTime, stateColors, stateLabels, sortPeers } from '../utils.js';

    let { status, deviceInfo, diagnostics = null } = $props();

    let dotColor = $derived(stateColors[status.backendState] || 'bg-text-secondary');
    let stateLabel = $derived(stateLabels[status.backendState] || status.backendState);
    let selfPeer = $derived(status.self);
    let peers = $derived(sortPeers(status.peers ?? []));

    let controlHost = $derived.by(() => {
        try { return new URL(status.controlURL).host; } catch { return status.controlURL; }
    });

    let preferredRegion = $derived.by(() => {
        if (status.derp?.length) {
            return status.derp.find(r => r.preferred) ?? null;
        }
        if (!diagnostics?.derpRegions?.length || !diagnostics.preferredDERP) return null;
        const dr = diagnostics.derpRegions.find(r => r.regionID === diagnostics.preferredDERP);
        return dr ? { regionID: dr.regionID, regionCode: dr.regionCode, regionName: dr.regionName, latencyMs: dr.latencyMs, preferred: true } : null;
    });

    let nearbyRegions = $derived.by(() => {
        if (!status.derp?.length) return [];
        return status.derp.filter(r => !r.preferred).slice(0, 3);
    });

    function latencyClass(ms) {
        if (ms < 15) return 'text-success';
        if (ms < 50) return 'text-blue';
        if (ms < 100) return 'text-warning';
        return 'text-text-tertiary';
    }

</script>

<section class="bg-surface rounded-xl border border-border overflow-hidden flex-1 min-h-0 flex flex-col">
    <div class="flex flex-col flex-1 min-h-0">
        <div class="p-4 flex flex-col">
            <div class="flex items-center justify-between mb-3">
                <h3 class="flex items-center gap-1.5 text-caption font-bold text-text-secondary uppercase tracking-wider">
                    <Icon name="tailscale" size={14} class="shrink-0" /><span>Tailscale</span>
                </h3>
                <span class="flex items-center gap-1.5">
                    <span class="w-2 h-2 rounded-full shrink-0 {dotColor}"></span>
                    <span class="text-caption font-bold text-text-heading">{stateLabel}</span>
                </span>
            </div>

            <div class="flex flex-col gap-1.5 text-body">
                {#if selfPeer?.hostName || deviceInfo?.model}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Hostname</span>
                        <span class="text-text truncate ml-4">{selfPeer?.hostName || deviceInfo?.model || '—'}</span>
                    </div>
                {/if}

                {#if status.tailscaleIPs?.[0]}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Tailscale IP</span>
                        <span class="text-text font-mono text-caption">{status.tailscaleIPs[0]}</span>
                    </div>
                {/if}

                {#if status.tailnetName}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Tailnet</span>
                        <span class="text-text truncate ml-4">{status.tailnetName}</span>
                    </div>
                {/if}

                {#if status.controlURL}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Control Server</span>
                        <span class="text-text truncate ml-4 text-caption font-mono">{controlHost}</span>
                    </div>
                {/if}

                {#if status.version}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Version</span>
                        <span class="text-text">{status.version}</span>
                    </div>
                {/if}

                <div class="flex justify-between">
                    <span class="text-text-secondary shrink-0">DERP</span>
                    {#if preferredRegion}
                        <span class="text-text font-mono text-caption flex items-center gap-1.5">
                            <span class="w-1.5 h-1.5 rounded-full bg-success shrink-0"></span>
                            {preferredRegion.regionCode}
                            <span class="text-success">{preferredRegion.latencyMs.toFixed(0)}ms</span>
                        </span>
                    {:else}
                        <span class="text-text-tertiary text-caption italic animate-pulse">...</span>
                    {/if}
                </div>
                {#if nearbyRegions.length > 0}
                    <div class="flex justify-end gap-2.5 -mt-0.5">
                        {#each nearbyRegions as r (r.regionID)}
                            <span class="text-caption font-mono text-text-tertiary/70">
                                {r.regionCode}
                                <span class={latencyClass(r.latencyMs)}>{r.latencyMs.toFixed(0)}ms</span>
                            </span>
                        {/each}
                    </div>
                {/if}

                {#if deviceInfo?.model}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Device</span>
                        <span class="text-text">{deviceInfo.model}</span>
                    </div>
                {/if}

                {#if deviceInfo?.firmware}
                    <div class="flex justify-between">
                        <span class="text-text-secondary shrink-0">Firmware</span>
                        <span class="text-text">{deviceInfo.firmware}</span>
                    </div>
                {/if}
            </div>

            {#if selfPeer && (selfPeer.rxBytes != null || selfPeer.txBytes != null)}
                <div class="mt-3 pt-3 border-t border-border">
                    <h4 class="text-caption font-bold text-text-secondary uppercase tracking-wider mb-2.5">Traffic</h4>
                    <div class="flex gap-6 text-body">
                        {#if selfPeer.rxBytes != null}
                            <div class="flex gap-2">
                                <span class="text-text-secondary">RX</span>
                                <span class="text-text">{formatBytes(selfPeer.rxBytes)}</span>
                            </div>
                        {/if}
                        {#if selfPeer.txBytes != null}
                            <div class="flex gap-2">
                                <span class="text-text-secondary">TX</span>
                                <span class="text-text">{formatBytes(selfPeer.txBytes)}</span>
                            </div>
                        {/if}
                    </div>
                </div>
            {/if}
        </div>

        <div class="flex flex-col min-h-0 border-t border-border">
            <h3 class="text-caption font-bold text-text-secondary uppercase tracking-wider px-4 pt-4 pb-2">
                Peers ({peers.length})
            </h3>
            {#if peers.length === 0}
                <p class="px-4 pb-4 text-body text-text-secondary">No peers connected</p>
            {:else}
                <div class="overflow-y-auto flex-1">
                    <div class="divide-y divide-border/50">
                        {#each peers as peer (peer.tailscaleIP)}
                            <div class="px-4 py-2.5 {peer.online ? '' : 'opacity-40'}">
                                <div class="flex items-center justify-between">
                                    <span class="text-body text-text truncate">{peer.hostName || peer.dnsName}</span>
                                    <span class="shrink-0 ml-2">
                                        {#if !peer.online}
                                            <span class="text-caption text-text-secondary">Offline</span>
                                        {:else if peer.curAddr}
                                            <span class="text-caption text-success">Direct</span>
                                        {:else if peer.peerRelay}
                                            <span class="text-caption text-blue">Peer Relay</span>
                                        {:else if peer.relay}
                                            <span class="text-caption text-warning">DERP</span>
                                        {:else}
                                            <span class="text-caption text-text-secondary">Offline</span>
                                        {/if}
                                    </span>
                                </div>
                                <div class="flex items-center gap-3 mt-0.5 text-caption text-text-secondary">
                                    <span class="font-mono">{peer.tailscaleIP}</span>
                                    <span>{peer.os}</span>
                                    {#if peer.online}
                                        <span class="text-success">Now</span>
                                    {:else}
                                        <span>{relativeTime(peer.lastSeen)}</span>
                                    {/if}
                                </div>
                            </div>
                        {/each}
                    </div>
                </div>
            {/if}
        </div>
    </div>
</section>
