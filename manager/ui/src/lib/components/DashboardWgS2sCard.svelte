<script>
    import Icon from './Icon.svelte';
    import { formatBytes, relativeTime, tunnelStatusInfo } from '../utils.js';

    let { tunnels = [], wgDiag = null } = $props();

    let enabledCount = $derived(tunnels.filter(t => t.enabled).length);
    let connectedCount = $derived(tunnels.filter(t => t.connected).length);
    let totalRx = $derived(tunnels.reduce((sum, t) => sum + (t.transferRx || 0), 0));
    let totalTx = $derived(tunnels.reduce((sum, t) => sum + (t.transferTx || 0), 0));

    let diagMap = $derived.by(() => {
        const map = {};
        if (wgDiag?.tunnels) {
            for (const t of wgDiag.tunnels) map[t.id] = t;
        }
        return map;
    });

    let statusDot = $derived(
        connectedCount > 0 ? 'bg-success' :
        enabledCount > 0 ? 'bg-warning' :
        'bg-text-secondary'
    );
    let statusLabel = $derived(
        connectedCount > 0 ? `${connectedCount} connected` :
        enabledCount > 0 ? 'No handshake' :
        tunnels.length > 0 ? 'All disabled' : 'No tunnels'
    );

    function tunnelHealthDots(tunnel) {
        const diag = diagMap[tunnel.id];
        return [
            { key: 'if', label: 'Interface', ok: diag ? diag.interfaceUp : null },
            { key: 'rt', label: 'Routes', ok: diag ? diag.routesOk : null },
            { key: 'fw', label: 'Forward rule', ok: tunnel.forwardINOk ?? diag?.forwardINOk ?? null },
            { key: 'pr', label: 'Peer', ok: tunnel.connected },
        ];
    }

    function healthTitle(dots) {
        return dots.map(d => `${d.key}: ${d.ok === null ? '?' : d.ok ? '✓' : '✗'}`).join('  ');
    }
</script>

<section class="bg-surface rounded-xl border border-border overflow-hidden flex-1 min-h-0 flex flex-col">
    <div class="flex flex-col flex-1 min-h-0">
        <div class="p-4 flex flex-col">
            <div class="flex items-center justify-between mb-3">
                <h3 class="flex items-center gap-1.5 text-caption font-bold text-text-secondary uppercase tracking-wider">
                    <Icon name="wireguard" size={14} class="shrink-0" /><span>WireGuard S2S</span>
                </h3>
                <span class="flex items-center gap-2.5">
                    <span class="flex items-center gap-1.5">
                        <span class="w-2 h-2 rounded-full shrink-0 {statusDot}"></span>
                        <span class="text-caption font-bold text-text-heading">{statusLabel}</span>
                    </span>
                    {#if wgDiag != null}
                        <span class="flex items-center gap-1 text-micro text-text-tertiary" title="Kernel module wireguard.ko">
                            <Icon name={wgDiag.wireguardModule ? 'check' : 'x'} size={10}
                                class={wgDiag.wireguardModule ? 'text-success' : 'text-error'} />
                            wg.ko
                        </span>
                    {/if}
                </span>
            </div>

            <div class="flex flex-col gap-1.5 text-body">
                <div class="flex justify-between">
                    <span class="text-text-secondary">Tunnels</span>
                    <span class="text-text">{tunnels.length}</span>
                </div>

                {#if enabledCount !== tunnels.length}
                    <div class="flex justify-between">
                        <span class="text-text-secondary">Enabled</span>
                        <span class="text-text">{enabledCount}</span>
                    </div>
                {/if}

                <div class="flex justify-between">
                    <span class="text-text-secondary">Connected</span>
                    <span class="text-text">{connectedCount}</span>
                </div>
            </div>

            {#if totalRx > 0 || totalTx > 0}
                <div class="mt-3 pt-3 border-t border-border">
                    <h4 class="text-caption font-bold text-text-secondary uppercase tracking-wider mb-2.5">Traffic</h4>
                    <div class="flex gap-6 text-body">
                        <div class="flex gap-2">
                            <span class="text-text-secondary">RX</span>
                            <span class="text-text">{formatBytes(totalRx)}</span>
                        </div>
                        <div class="flex gap-2">
                            <span class="text-text-secondary">TX</span>
                            <span class="text-text">{formatBytes(totalTx)}</span>
                        </div>
                    </div>
                </div>
            {/if}
        </div>

        <div class="flex flex-col min-h-0 border-t border-border">
            <h3 class="text-caption font-bold text-text-secondary uppercase tracking-wider px-4 pt-4 pb-2">
                Tunnels ({tunnels.length})
            </h3>

            {#if tunnels.length === 0}
                <p class="px-4 pb-4 text-body text-text-secondary">No tunnels configured</p>
            {:else}
                <div class="overflow-y-auto flex-1">
                    <div class="divide-y divide-border/50">
                        {#each tunnels as tunnel (tunnel.id || tunnel.name)}
                            {@const si = tunnelStatusInfo(tunnel)}
                            {@const dots = tunnelHealthDots(tunnel)}
                            <div class="flex items-center gap-3 px-4 py-2.5 hover:bg-surface-hover transition-colors {!tunnel.enabled ? 'opacity-40' : ''}">
                                <span class="w-2 h-2 rounded-full shrink-0 {si.dot}" title={si.label}></span>
                                <div class="flex-1 min-w-0">
                                    <div class="flex items-center gap-2">
                                        <span class="text-body text-text truncate">{tunnel.name}</span>
                                        {#if !tunnel.enabled}
                                            <span class="text-micro text-text-tertiary uppercase tracking-wider">disabled</span>
                                        {/if}
                                    </div>
                                    <div class="flex items-center gap-2 mt-0.5">
                                        <span class="text-caption text-text-secondary">
                                            {tunnel.endpoint || tunnel.peerEndpoint || '—'}
                                        </span>
                                        {#if tunnel.enabled}
                                            <span class="flex items-center gap-0.5" title={healthTitle(dots)}>
                                                {#each dots as d (d.key)}
                                                    <span class="w-[5px] h-[5px] rounded-full
                                                        {d.ok === null ? 'bg-text-tertiary/50' :
                                                         d.ok ? 'bg-success' : 'bg-error'}"></span>
                                                {/each}
                                            </span>
                                        {/if}
                                    </div>
                                </div>
                                <div class="text-right shrink-0">
                                    <div class="text-caption text-text-secondary">
                                        {relativeTime(tunnel.lastHandshake)}
                                    </div>
                                    {#if tunnel.transferRx != null && tunnel.transferRx > 0}
                                        <div class="text-micro text-text-tertiary mt-0.5">
                                            ↓{formatBytes(tunnel.transferRx)} ↑{formatBytes(tunnel.transferTx)}
                                        </div>
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
