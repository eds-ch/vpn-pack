<script>
    import Icon from './Icon.svelte';
    import StatusPill from './StatusPill.svelte';
    import WgS2sPill from './WgS2sPill.svelte';
    import { REPO_INSTALL_URL } from '../constants.js';

    let { hostname = '', status, changedFields, onThemeToggle, onNavigateIntegration = null, updateInfo = null, onDismissUpdate = null } = $props();
    let isLight = $state(document.documentElement.classList.contains('light'));
    let bannerDismissed = $state(false);

    let isNeedsLogin = $derived(status.backendState === 'NeedsLogin');
    let zbfDisabled = $derived(
        status.integrationStatus?.configured && status.integrationStatus?.zbfEnabled === false
    );
    let showBanner = $derived(
        ((isNeedsLogin || !bannerDismissed) && status.integrationStatus && !status.integrationStatus.configured)
        || (!bannerDismissed && zbfDisabled)
    );

    let showUpdateBanner = $derived(
        updateInfo?.available && !updateInfo?.dismissed
    );

    function toggleTheme() {
        onThemeToggle();
        isLight = document.documentElement.classList.contains('light');
    }
</script>

<header class="bg-panel border-b border-border h-[50px] px-3 md:px-4 grid grid-cols-[auto_1fr_auto] items-center shrink-0">
    <div class="flex items-center gap-2">
        <svg class="w-5 h-5" viewBox="0 0 128 128">
            <line x1="36" y1="92" x2="92" y2="36" stroke="#4797FF" stroke-width="16" stroke-linecap="round" />
            <circle cx="92" cy="36" r="20" fill="#4797FF" />
            <circle cx="36" cy="92" r="20" fill="#4797FF" />
            <rect x="16" y="16" width="40" height="40" rx="12" fill="#006FFF" />
            <rect x="72" y="72" width="40" height="40" rx="12" fill="#006FFF" />
        </svg>
        <span class="text-body font-bold text-text-heading">VPN Pack</span>
    </div>

    <div class="flex justify-center">
        {#if hostname}
            <span class="text-caption text-text-secondary hidden md:inline truncate max-w-48">{hostname}</span>
        {/if}
    </div>

    <div class="flex items-center gap-2 md:gap-3 justify-end">
        <WgS2sPill tunnels={status.wgS2sTunnels ?? []} />
        {#if (status.wgS2sTunnels ?? []).length > 0}
            <span class="w-px h-3.5 bg-border"></span>
        {/if}
        <StatusPill {status} {changedFields} />
        <span class="w-px h-3.5 bg-border"></span>
        <button
            onclick={toggleTheme}
            class="w-8 h-8 flex items-center justify-center rounded text-text-secondary hover:text-text hover:bg-surface-hover transition-colors"
            aria-label="Toggle theme"
            title="Toggle theme"
        >
            <Icon name={isLight ? 'moon' : 'sun'} size={16} />
        </button>
    </div>
</header>

{#if showBanner}
    <div class="bg-warning/10 border-b border-warning/20 px-3 md:px-4 py-2 flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 min-w-0">
            <Icon name="alert-triangle" size={14} class="text-warning shrink-0" />
            <span class="text-caption text-warning truncate">
                {#if zbfDisabled}
                    Zone-Based Firewall required. In UniFi Network go to Settings → Firewall & Security and click "Upgrade to the New Zone-Based Firewall".
                {:else if isNeedsLogin}
                    Integration API key required to activate Tailscale.
                {:else}
                    UniFi API key not configured. Firewall zones are managed via legacy UDAPI only.
                {/if}
            </span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
            {#if onNavigateIntegration}
                <button
                    onclick={onNavigateIntegration}
                    class="text-caption font-bold text-warning hover:text-warning/80 underline underline-offset-2 whitespace-nowrap transition-colors"
                >
                    Configure in Settings
                </button>
            {/if}
            {#if !isNeedsLogin || zbfDisabled}
                <button
                    onclick={() => bannerDismissed = true}
                    class="p-0.5 text-warning/60 hover:text-warning transition-colors"
                    aria-label="Dismiss"
                >
                    <Icon name="x" size={14} />
                </button>
            {/if}
        </div>
    </div>
{/if}

{#if showUpdateBanner}
    <div class="bg-info/10 border-b border-info/20 px-3 md:px-4 py-2 flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 min-w-0">
            <Icon name="download" size={14} class="text-info shrink-0" />
            <span class="text-caption text-info truncate">
                Version {updateInfo.version} available.
                Update via SSH: <code class="bg-info/10 px-1 rounded text-caption font-mono">curl -fsSL {REPO_INSTALL_URL} | sh</code>
            </span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
            {#if updateInfo.changelogURL}
                <a href={updateInfo.changelogURL} target="_blank" rel="noopener"
                   class="text-caption font-bold text-info hover:text-info/80 underline underline-offset-2 whitespace-nowrap transition-colors">
                    Release notes
                </a>
            {/if}
            <button
                onclick={() => onDismissUpdate?.()}
                class="p-0.5 text-info/60 hover:text-info transition-colors"
                aria-label="Dismiss"
            >
                <Icon name="x" size={14} />
            </button>
        </div>
    </div>
{/if}
