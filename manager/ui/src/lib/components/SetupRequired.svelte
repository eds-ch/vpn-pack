<script>
    import Icon from './Icon.svelte';
    import ApiKeyForm from './ApiKeyForm.svelte';
    import { setIntegrationApiKey } from '../api.js';

    let apiKey = $state('');
    let loading = $state(false);

    async function handleSave() {
        if (!apiKey.trim()) return;
        loading = true;
        const result = await setIntegrationApiKey(apiKey.trim());
        if (result) {
            apiKey = '';
        }
        loading = false;
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
            <ApiKeyForm bind:value={apiKey} disabled={loading} onEnter={handleSave} id="setup-api-key" />
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
