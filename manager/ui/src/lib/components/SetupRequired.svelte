<script>
    import Icon from './Icon.svelte';
    import { setIntegrationApiKey } from '../api.js';

    let apiKey = $state('');
    let showKey = $state(false);
    let loading = $state(false);

    async function handleSave() {
        if (!apiKey.trim()) return;
        loading = true;
        const result = await setIntegrationApiKey(apiKey.trim());
        if (result) {
            apiKey = '';
            showKey = false;
        }
        loading = false;
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') handleSave();
    }
</script>

<section class="bg-surface rounded-xl border border-warning/30 overflow-hidden">
    <div class="bg-warning/8 border-b border-warning/15 px-5 py-3 flex items-center gap-2.5">
        <Icon name="alert-triangle" size={16} class="text-warning shrink-0" />
        <h3 class="text-body font-bold text-text-heading">Setup Required</h3>
    </div>

    <div class="p-5 space-y-4">
        <p class="text-caption text-text-secondary leading-relaxed">
            A UniFi Integration API key is required before activating Tailscale.
            The key enables proper firewall zone management, ensuring rules persist
            across reboots and firmware updates.
        </p>

        <div class="space-y-2">
            <label for="setup-api-key" class="text-body text-text">API Key</label>
            <div class="relative">
                <input
                    id="setup-api-key"
                    type={showKey ? 'text' : 'password'}
                    bind:value={apiKey}
                    onkeydown={handleKeydown}
                    placeholder="Enter UniFi API key"
                    disabled={loading}
                    class="w-full px-3 py-2 pr-10 text-body rounded-lg border border-border bg-input text-text placeholder-text-tertiary focus:outline-none focus:border-blue font-mono"
                />
                <button
                    type="button"
                    onclick={() => showKey = !showKey}
                    class="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-text-tertiary hover:text-text-secondary transition-colors"
                    aria-label={showKey ? 'Hide key' : 'Show key'}
                >
                    <Icon name={showKey ? 'eye-off' : 'eye'} size={16} />
                </button>
            </div>
            <p class="text-caption text-text-tertiary">
                Create at <span class="text-text-secondary">unifi.ui.com</span> &rarr; Settings &rarr; API
            </p>
        </div>

        <button
            onclick={handleSave}
            disabled={!apiKey.trim() || loading}
            class="px-4 py-2 rounded-lg text-body font-bold bg-blue text-white transition-colors
                {apiKey.trim() && !loading ? 'hover:bg-blue-hover' : 'opacity-50 cursor-not-allowed'}"
        >
            {loading ? 'Saving...' : 'Save & Continue'}
        </button>
    </div>
</section>
