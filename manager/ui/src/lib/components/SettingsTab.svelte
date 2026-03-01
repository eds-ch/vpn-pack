<script>
    import { setSettings } from '../api.js';
    import SettingsGeneral from './SettingsGeneral.svelte';
    import SettingsAdvanced from './SettingsAdvanced.svelte';
    import SettingsIntegration from './SettingsIntegration.svelte';
    import RoutingTab from './RoutingTab.svelte';
    import WgS2sTab from './WgS2sTab.svelte';
    import Icon from './Icon.svelte';

    let { status, deviceInfo, subTab = 'general', onSubTabChange } = $props();

    const SETTINGS_KEYS = ['hostname', 'acceptDNS', 'acceptRoutes', 'shieldsUp', 'runSSH',
        'controlURL', 'noSNAT', 'udpPort', 'relayServerPort', 'relayServerEndpoints', 'advertiseTags'];

    let serverSettings = $derived(
        Object.fromEntries(SETTINGS_KEYS.map(k => [k, status[k]]))
    );

    let dirtyOverrides = $state({});

    let displaySettings = $derived({ ...serverSettings, ...dirtyOverrides });

    let loading = $derived(status.backendState === 'Unknown');
    let saving = $state(false);

    let generalHasErrors = $state(false);
    let advancedHasErrors = $state(false);
    let hasValidationErrors = $derived(generalHasErrors || advancedHasErrors);

    let hasChanges = $derived(
        Object.keys(dirtyOverrides).some(k =>
            JSON.stringify(dirtyOverrides[k]) !== JSON.stringify(serverSettings[k])
        )
    );

    const tailscaleTabs = [
        { id: 'general', label: 'General' },
        { id: 'advanced', label: 'Advanced' },
        { id: 'routing', label: 'Routing' },
    ];

    const wireguardTabs = [
        { id: 'tunnels', label: 'S2S Tunnels' },
    ];

    const unifiTabs = [
        { id: 'integration', label: 'Integration' },
    ];

    let showApply = $derived(subTab === 'general' || subTab === 'advanced');

    function stageChange(key, value) {
        if (JSON.stringify(value) === JSON.stringify(serverSettings[key])) {
            const { [key]: _, ...rest } = dirtyOverrides;
            dirtyOverrides = rest;
        } else {
            dirtyOverrides = { ...dirtyOverrides, [key]: value };
        }
    }

    async function handleApply() {
        saving = true;
        const delta = {};
        for (const [key, value] of Object.entries(dirtyOverrides)) {
            if (JSON.stringify(value) !== JSON.stringify(serverSettings[key])) {
                delta[key] = value;
            }
        }
        const result = await setSettings(delta);
        if (result) {
            dirtyOverrides = {};
        }
        saving = false;
    }
</script>

<div class="flex flex-col md:flex-row gap-4 md:gap-6">
    <!-- Desktop: sectioned sidebar -->
    <nav class="hidden md:flex flex-col w-44 shrink-0">
        <h2 class="text-title text-text-heading px-3 mb-5">Settings</h2>

        <span class="flex items-center gap-1.5 px-3 pb-1.5 text-caption font-bold text-text-tertiary uppercase tracking-wider">
            <Icon name="tailscale" size={14} class="shrink-0 text-text-secondary" /><span>Tailscale</span>
        </span>
        <div class="flex flex-col gap-0.5">
            {#each tailscaleTabs as tab (tab.id)}
                <button
                    class="text-left px-3 py-2 text-body rounded-lg transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => onSubTabChange(tab.id)}
                >
                    {tab.label}
                </button>
            {/each}
        </div>

        <div class="my-3 mx-3 border-t border-border"></div>

        <span class="flex items-center gap-1.5 px-3 pb-1.5 text-caption font-bold text-text-tertiary uppercase tracking-wider">
            <Icon name="wireguard" size={14} class="shrink-0 text-text-secondary" /><span>WireGuard</span>
        </span>
        <div class="flex flex-col gap-0.5">
            {#each wireguardTabs as tab (tab.id)}
                <button
                    class="text-left px-3 py-2 text-body rounded-lg transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => onSubTabChange(tab.id)}
                >
                    {tab.label}
                </button>
            {/each}
        </div>

        <div class="my-3 mx-3 border-t border-border"></div>

        <span class="flex items-center gap-1.5 px-3 pb-1.5 text-caption font-bold text-text-tertiary uppercase tracking-wider">
            <Icon name="ubiquiti" size={14} class="shrink-0 text-text-secondary" /><span>UniFi</span>
        </span>
        <div class="flex flex-col gap-0.5">
            {#each unifiTabs as tab (tab.id)}
                <button
                    class="text-left px-3 py-2 text-body rounded-lg transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => onSubTabChange(tab.id)}
                >
                    {tab.label}
                </button>
            {/each}
        </div>
    </nav>

    <div class="flex-1 min-w-0">
        <!-- Mobile: horizontal navigation pills -->
        <div class="flex md:hidden gap-1 overflow-x-auto mb-4">
            {#each tailscaleTabs as tab (tab.id)}
                <button
                    class="px-3 py-1.5 text-body rounded-lg whitespace-nowrap transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => onSubTabChange(tab.id)}
                >{tab.label}</button>
            {/each}
            <div class="w-px bg-border shrink-0 my-1"></div>
            {#each wireguardTabs as tab (tab.id)}
                <button
                    class="px-3 py-1.5 text-body rounded-lg whitespace-nowrap transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => onSubTabChange(tab.id)}
                >{tab.label}</button>
            {/each}
            <div class="w-px bg-border shrink-0 my-1"></div>
            {#each unifiTabs as tab (tab.id)}
                <button
                    class="px-3 py-1.5 text-body rounded-lg whitespace-nowrap transition-all duration-150
                        {subTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => onSubTabChange(tab.id)}
                >{tab.label}</button>
            {/each}
        </div>

        {#if subTab === 'general' || subTab === 'advanced'}
            {#if loading}
                <div class="space-y-4">
                    {#each [1, 2, 3] as _, i (i)}
                        <div class="h-14 bg-surface rounded-lg animate-pulse"></div>
                    {/each}
                </div>
            {:else}
                {#if subTab === 'general'}
                    <SettingsGeneral staged={displaySettings} original={serverSettings} {stageChange} effectiveHostname={status.self?.hostName || ''} onValidation={(v) => generalHasErrors = v} />
                {:else}
                    <SettingsAdvanced staged={displaySettings} original={serverSettings} {stageChange} onValidation={(v) => advancedHasErrors = v} />
                {/if}
            {/if}
        {:else if subTab === 'routing'}
            <RoutingTab {status} {deviceInfo} />
        {:else if subTab === 'integration'}
            <SettingsIntegration {status} />
        {:else if subTab === 'tunnels'}
            <WgS2sTab {status} />
        {/if}

        {#if showApply && !loading}
            <div class="flex justify-end mt-6">
                <button
                    onclick={handleApply}
                    disabled={!hasChanges || saving || hasValidationErrors}
                    class="px-6 py-2 rounded-lg text-body font-bold bg-blue text-white transition-colors
                        {hasChanges && !saving && !hasValidationErrors ? 'hover:bg-blue-hover' : 'opacity-50 cursor-not-allowed'}"
                >
                    {saving ? 'Applying...' : 'Apply'}
                </button>
            </div>
        {/if}
    </div>
</div>
