<script>
    import { onMount } from 'svelte';
    import { getDiagnostics, getFirewallStatus } from '../api.js';
    import DashboardTailscaleCard from './DashboardTailscaleCard.svelte';
    import DashboardWgS2sCard from './DashboardWgS2sCard.svelte';
    import ConnectionFlow from './ConnectionFlow.svelte';
    import SetupRequired from './SetupRequired.svelte';
    import DeviceInfo from './DeviceInfo.svelte';
    import Icon from './Icon.svelte';

    let { status, deviceInfo, onNavigateIntegration = null } = $props();

    let integrationConfigured = $derived(status.integrationStatus?.configured ?? false);

    const STORAGE_KEY = 'dashboard-split';
    const MIN = 0.2, MAX = 0.8;

    let ratio = $state(+(localStorage.getItem(STORAGE_KEY) ?? 0.5));
    let dragging = $state(false);
    let container = $state();

    let diagnostics = $state(null);
    let fw = $state(null);
    let diagLoading = $state(false);

    async function refreshDiagnostics() {
        diagLoading = true;
        const [diagData, fwData] = await Promise.all([
            getDiagnostics(),
            getFirewallStatus(),
        ]);
        if (diagData) diagnostics = diagData;
        if (fwData) fw = fwData;
        diagLoading = false;
    }

    onMount(() => { refreshDiagnostics(); });

    function onPointerDown(e) {
        dragging = true;
        e.currentTarget.setPointerCapture(e.pointerId);
    }

    function onPointerMove(e) {
        if (!dragging) return;
        const rect = container.getBoundingClientRect();
        ratio = Math.max(MIN, Math.min(MAX, (e.clientX - rect.left) / rect.width));
    }

    function onPointerUp() {
        if (!dragging) return;
        dragging = false;
        localStorage.setItem(STORAGE_KEY, ratio.toFixed(3));
    }

    function onDblClick() {
        ratio = 0.5;
        localStorage.setItem(STORAGE_KEY, ratio.toFixed(3));
    }
</script>

{#if status.backendState === 'Running'}
    <!-- Mobile: scrollable stacked cards -->
    <div class="md:hidden flex flex-col gap-3 flex-1 min-h-0 overflow-y-auto">
        <DashboardTailscaleCard {status} {deviceInfo} {diagnostics} />
        <DashboardWgS2sCard tunnels={status.wgS2sTunnels ?? []} wgDiag={diagnostics?.wgS2s ?? null} />
    </div>

    <!-- Desktop: resizable side-by-side -->
    <div
        class="hidden md:flex flex-row flex-1 min-h-0"
        class:select-none={dragging}
        bind:this={container}
    >
        <div style="flex: {ratio}" class="min-w-0 min-h-0 flex flex-col overflow-y-auto">
            <DashboardTailscaleCard {status} {deviceInfo} {diagnostics} />
        </div>

        <div
            class="shrink-0 flex flex-col items-center justify-center cursor-col-resize select-none px-1.5 group"
            onpointerdown={onPointerDown}
            onpointermove={onPointerMove}
            onpointerup={onPointerUp}
            onpointercancel={onPointerUp}
            ondblclick={onDblClick}
            role="separator"
            aria-orientation="vertical"
            title="Drag to resize, double-click to reset"
        >
            <div class="flex flex-col items-center gap-2 transition-opacity {dragging ? 'opacity-60' : 'opacity-0 group-hover:opacity-40'}">
                <span class="w-1 h-1 rounded-full bg-text-secondary"></span>
                <span class="w-1 h-1 rounded-full bg-text-secondary"></span>
                <span class="w-1 h-1 rounded-full bg-text-secondary"></span>
            </div>
            <button
                onclick={(e) => { e.stopPropagation(); refreshDiagnostics(); }}
                disabled={diagLoading}
                class="mt-2 p-1 rounded text-text-tertiary hover:text-text-secondary disabled:opacity-50 transition-colors opacity-0 group-hover:opacity-100"
                title="Refresh diagnostics"
            >
                <Icon name="refresh-cw" size={12} class={diagLoading ? 'animate-spin' : ''} />
            </button>
        </div>

        <div style="flex: {1 - ratio}" class="min-w-0 min-h-0 flex flex-col overflow-y-auto">
            <DashboardWgS2sCard tunnels={status.wgS2sTunnels ?? []} wgDiag={diagnostics?.wgS2s ?? null} />
        </div>
    </div>
{:else if status.backendState === 'NeedsLogin'}
    <div class="space-y-4">
        <DeviceInfo {deviceInfo} />
        {#if integrationConfigured}
            <ConnectionFlow {status} />
        {:else}
            <SetupRequired />
        {/if}
    </div>
{:else if status.backendState === 'Stopped'}
    <div class="space-y-4">
        <DeviceInfo {deviceInfo} />
        <section class="bg-surface rounded-xl p-6 border border-border text-center">
            <span class="inline-flex items-center gap-2">
                <span class="w-2.5 h-2.5 rounded-full bg-error"></span>
                <span class="text-body text-text-secondary">Tailscale is stopped</span>
            </span>
        </section>
    </div>
{:else}
    <div class="space-y-4">
        <DeviceInfo {deviceInfo} />
        <section class="bg-surface rounded-xl p-6 border border-border text-center">
            <p class="text-body text-text-secondary animate-pulse">Waiting for tailscaled...</p>
        </section>
    </div>
{/if}
