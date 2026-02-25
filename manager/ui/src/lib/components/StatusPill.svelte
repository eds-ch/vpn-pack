<script>
    import { stateColors } from '../utils.js';

    let { status, changedFields } = $props();
    let open = $state(false);
    let root = $state(null);

    let dotColor = $derived(stateColors[status.backendState] || 'bg-text-secondary');
    let primaryIP = $derived(status.tailscaleIPs?.[0] ?? '');
    let health = $derived(status.firewallHealth);

    let healthChecks = $derived(health ? [
        { label: 'Firewall Zone', ok: health.zoneActive,
          desc: health.zoneActive ? 'tailscale0 → VPN_IN zone' : 'Not in firewall zone' },
        { label: 'Watcher', ok: health.watcherRunning,
          desc: health.watcherRunning ? 'Monitoring config push events' : 'Rules won\'t auto-restore' },
        { label: 'UDAPI Socket', ok: health.udapiReachable,
          desc: health.udapiReachable ? 'Socket connected' : 'Cannot reach udapi-server' },
    ] : []);

    let healthIssues = $derived(healthChecks.filter(c => !c.ok).length);

    function handleWindowClick(e) {
        if (open && root && !root.contains(e.target)) open = false;
    }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={(e) => { if (open && e.key === 'Escape') open = false; }} />

<div class="relative" bind:this={root}>
    <button
        onclick={() => open = !open}
        class="flex items-center gap-1.5 py-1 text-caption cursor-pointer transition-colors
            hover:text-text-heading
            {open ? 'text-text-heading' : ''}"
        aria-expanded={open}
        aria-haspopup="dialog"
    >
        <span class="w-2 h-2 rounded-full {dotColor} shrink-0"></span>
        <span class="text-text">
            <span class="hidden sm:inline">Tailscale</span> {status.backendState}
        </span>
        {#if primaryIP}
            <span class="text-text-tertiary hidden sm:inline">·</span>
            <span class="text-text-secondary font-mono hidden sm:inline">{primaryIP}</span>
        {/if}
        {#if health && healthIssues > 0}
            <span class="ml-0.5 w-4 h-4 rounded-full bg-warning/20 text-warning text-micro font-bold flex items-center justify-center">{healthIssues}</span>
        {/if}
    </button>

    {#if open}
        <div class="popover-enter fixed md:absolute inset-x-3 md:inset-x-auto bottom-[calc(3.5rem+env(safe-area-inset-bottom,0px)+0.5rem)] md:bottom-auto md:right-0 md:top-full md:mt-2 z-50 bg-surface border border-border rounded-xl shadow-card md:w-72"
             role="dialog" aria-label="Tailscale status details">
            <div class="px-4 pt-4 pb-3">
                <div class="text-micro font-bold text-text-tertiary uppercase tracking-wider mb-3">Tailscale</div>
                <div class="space-y-2">
                    <div class="flex items-center justify-between">
                        <span class="text-caption text-text-secondary">State</span>
                        <div class="flex items-center gap-1.5">
                            <span class="w-1.5 h-1.5 rounded-full {dotColor}"></span>
                            <span class="text-caption text-text">{status.backendState}</span>
                        </div>
                    </div>
                    {#if primaryIP}
                        <div class="flex items-center justify-between">
                            <span class="text-caption text-text-secondary">Tailnet IP</span>
                            <span class="text-caption text-text font-mono">{primaryIP}</span>
                        </div>
                    {/if}
                    <div class="flex items-center justify-between">
                        <span class="text-caption text-text-secondary">Data Stream</span>
                        <div class="flex items-center gap-1.5">
                            <span class="w-1.5 h-1.5 rounded-full {status.connected ? 'bg-success' : 'bg-error'}"></span>
                            <span class="text-caption text-text">{status.connected ? 'Connected' : 'Disconnected'}</span>
                        </div>
                    </div>
                </div>
            </div>

            {#if health}
                <div class="border-t border-border"></div>
                <div class="px-4 pt-3 pb-4">
                    <div class="text-micro font-bold text-text-tertiary uppercase tracking-wider mb-3">Integration Health</div>
                    <div class="space-y-3">
                        {#each healthChecks as check (check.label)}
                            <div class="flex items-start gap-2.5">
                                <span class="w-2 h-2 rounded-full mt-0.5 shrink-0 {check.ok ? 'bg-success' : 'bg-error'}"></span>
                                <div>
                                    <div class="text-caption text-text">{check.label}</div>
                                    <div class="text-caption text-text-tertiary">{check.desc}</div>
                                </div>
                            </div>
                        {/each}
                    </div>
                </div>
            {/if}
        </div>
    {/if}
</div>

<style>
    @keyframes popover-enter {
        from { opacity: 0; transform: translateY(-4px); }
        to { opacity: 1; transform: translateY(0); }
    }
    .popover-enter {
        animation: popover-enter 150ms ease-out;
    }
</style>
