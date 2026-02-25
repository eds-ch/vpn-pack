<script>
    import { onMount } from 'svelte';
    import { getSettings, setSettings } from '../api.js';
    import SettingsGeneral from './SettingsGeneral.svelte';
    import SettingsAdvanced from './SettingsAdvanced.svelte';
    import SettingsIntegration from './SettingsIntegration.svelte';
    import RoutingTab from './RoutingTab.svelte';
    import WgS2sTab from './WgS2sTab.svelte';
    import Icon from './Icon.svelte';

    let { status, deviceInfo, settingsTarget = null, onTargetConsumed = null } = $props();

    let loading = $state(true);
    let saving = $state(false);
    let original = $state({});
    let staged = $state({});
    let loadError = $state(false);

    let activeSubTab = $state('general');
    let generalHasErrors = $state(false);
    let advancedHasErrors = $state(false);
    let hasValidationErrors = $derived(generalHasErrors || advancedHasErrors);

    let hasChanges = $derived(
        Object.keys(staged).some(k => JSON.stringify(staged[k]) !== JSON.stringify(original[k]))
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

    let showApply = $derived(activeSubTab === 'general' || activeSubTab === 'advanced');

    $effect(() => {
        if (settingsTarget === 'integration') {
            activeSubTab = 'integration';
            onTargetConsumed?.();
        }
    });

    onMount(async () => {
        const data = await getSettings();
        if (data) {
            original = { ...data };
            staged = { ...data };
        } else {
            loadError = true;
        }
        loading = false;
    });

    function stageChange(key, value) {
        staged = { ...staged, [key]: value };
    }

    async function handleApply() {
        saving = true;
        const delta = {};
        for (const key of Object.keys(staged)) {
            if (JSON.stringify(staged[key]) !== JSON.stringify(original[key])) {
                delta[key] = staged[key];
            }
        }
        const result = await setSettings(delta);
        if (result) {
            original = { ...original, ...result };
            staged = { ...original };
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
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => activeSubTab = tab.id}
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
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => activeSubTab = tab.id}
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
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary hover:text-text hover:bg-surface-hover'}"
                    onclick={() => activeSubTab = tab.id}
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
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => activeSubTab = tab.id}
                >{tab.label}</button>
            {/each}
            <div class="w-px bg-border shrink-0 my-1"></div>
            {#each wireguardTabs as tab (tab.id)}
                <button
                    class="px-3 py-1.5 text-body rounded-lg whitespace-nowrap transition-all duration-150
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => activeSubTab = tab.id}
                >{tab.label}</button>
            {/each}
            <div class="w-px bg-border shrink-0 my-1"></div>
            {#each unifiTabs as tab (tab.id)}
                <button
                    class="px-3 py-1.5 text-body rounded-lg whitespace-nowrap transition-all duration-150
                        {activeSubTab === tab.id
                            ? 'text-blue font-bold bg-surface'
                            : 'text-text-secondary'}"
                    onclick={() => activeSubTab = tab.id}
                >{tab.label}</button>
            {/each}
        </div>

        {#if activeSubTab === 'general' || activeSubTab === 'advanced'}
            {#if loading}
                <div class="space-y-4">
                    {#each [1, 2, 3] as _, i (i)}
                        <div class="h-14 bg-surface rounded-lg animate-pulse"></div>
                    {/each}
                </div>
            {:else if loadError}
                <div class="rounded-xl border border-error/30 bg-error/5 p-6 text-center">
                    <p class="text-body text-error">Failed to load settings</p>
                </div>
            {:else}
                {#if activeSubTab === 'general'}
                    <SettingsGeneral {staged} {original} {stageChange} onValidation={(v) => generalHasErrors = v} />
                {:else}
                    <SettingsAdvanced {staged} {original} {stageChange} onValidation={(v) => advancedHasErrors = v} />
                {/if}
            {/if}
        {:else if activeSubTab === 'routing'}
            <RoutingTab {status} {deviceInfo} />
        {:else if activeSubTab === 'integration'}
            <SettingsIntegration {status} />
        {:else if activeSubTab === 'tunnels'}
            <WgS2sTab {status} />
        {/if}

        {#if showApply && !loading && !loadError}
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
