<script>
    import Icon from './Icon.svelte';
    import ApiKeyForm from './ApiKeyForm.svelte';
    import { setIntegrationApiKey, removeIntegrationApiKey, testIntegrationKey } from '../api.js';

    let { status } = $props();

    let apiKey = $state('');
    let loading = $state(false);
    let testResult = $state(null);
    let confirmRemove = $state(false);

    let integrationStatus = $derived(status.integrationStatus);
    let configured = $derived(integrationStatus?.configured ?? false);
    let valid = $derived(integrationStatus?.valid ?? false);

    async function handleSave() {
        if (!apiKey.trim()) return;
        loading = true;
        testResult = null;
        const result = await setIntegrationApiKey(apiKey.trim());
        if (result) {
            apiKey = '';
        }
        loading = false;
    }

    async function handleTest() {
        loading = true;
        testResult = null;
        const result = await testIntegrationKey();
        if (result) {
            testResult = result;
        }
        loading = false;
    }

    async function handleRemove() {
        if (!confirmRemove) {
            confirmRemove = true;
            return;
        }
        loading = true;
        await removeIntegrationApiKey();
        confirmRemove = false;
        testResult = null;
        loading = false;
    }
</script>

<div>
    <h2 class="text-heading text-text-heading">Integration</h2>
    <p class="text-caption text-text-tertiary mt-1">UniFi Network API for automatic firewall zone management</p>
</div>

<div class="divide-y divide-border mt-8">
    <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2 py-4 first:pt-0">
        <div>
            <span class="text-body text-text">API Key Status</span>
            <p class="text-caption text-text-tertiary mt-0.5">UniFi Network Integration API key for firewall zone management</p>
        </div>
        <div class="flex items-center gap-2">
            {#if configured && valid}
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-caption bg-success/15 text-success">
                    <Icon name="check" size={12} />
                    Configured
                </span>
            {:else if configured && !valid}
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-caption bg-error/15 text-error">
                    <Icon name="alert-triangle" size={12} />
                    Invalid
                </span>
            {:else}
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-caption bg-warning/15 text-warning">
                    <Icon name="alert-triangle" size={12} />
                    Not Configured
                </span>
            {/if}
        </div>
    </div>

    {#if configured}
        <div class="py-4 space-y-2">
            {#if integrationStatus?.siteId}
                <div class="flex justify-between text-body">
                    <span class="text-text-secondary">Site ID</span>
                    <span class="text-text font-mono text-caption">{integrationStatus.siteId}</span>
                </div>
            {/if}
            {#if integrationStatus?.appVersion}
                <div class="flex justify-between text-body">
                    <span class="text-text-secondary">UniFi Network</span>
                    <span class="text-text">v{integrationStatus.appVersion}</span>
                </div>
            {/if}
            {#if integrationStatus?.error}
                <div class="flex gap-2 items-start mt-2 p-2.5 rounded-lg bg-error/8 border border-error/20">
                    <Icon name="alert-triangle" size={14} class="text-error shrink-0 mt-0.5" />
                    <span class="text-caption text-error">{integrationStatus.error}</span>
                </div>
            {/if}
        </div>
    {/if}

    <div class="py-4">
        <div class="flex flex-col gap-2">
            <label for="apiKey" class="text-body text-text">
                {configured ? 'Replace API Key' : 'API Key'}
            </label>
            <ApiKeyForm bind:value={apiKey} disabled={loading} id="apiKey" />
        </div>
    </div>

    <div class="flex flex-wrap items-center gap-2 py-4">
        {#if !configured}
            <button
                onclick={handleSave}
                disabled={!apiKey.trim() || loading}
                class="px-4 py-1.5 rounded-lg text-body font-bold bg-blue text-white transition-colors
                    {apiKey.trim() && !loading ? 'hover:bg-blue-hover' : 'opacity-50 cursor-not-allowed'}"
            >
                {loading ? 'Saving...' : 'Save'}
            </button>
        {:else}
            <button
                onclick={handleSave}
                disabled={!apiKey.trim() || loading}
                class="px-4 py-1.5 rounded-lg text-body font-bold bg-blue text-white transition-colors
                    {apiKey.trim() && !loading ? 'hover:bg-blue-hover' : 'opacity-50 cursor-not-allowed'}"
            >
                {loading ? 'Saving...' : 'Update Key'}
            </button>
            <button
                onclick={handleTest}
                disabled={loading}
                class="px-4 py-1.5 rounded-lg text-body font-bold border border-border text-text hover:bg-surface-hover transition-colors
                    {loading ? 'opacity-50 cursor-not-allowed' : ''}"
            >
                {loading ? 'Testing...' : 'Test Connection'}
            </button>
            <button
                onclick={handleRemove}
                disabled={loading}
                class="px-4 py-1.5 rounded-lg text-body font-bold transition-colors
                    {confirmRemove
                        ? 'bg-error/15 text-error border border-error/30 hover:bg-error/25'
                        : 'border border-border text-text-secondary hover:text-error hover:border-error/30'}"
            >
                {confirmRemove ? 'Confirm Remove' : 'Remove'}
            </button>
            {#if confirmRemove}
                <button
                    onclick={() => confirmRemove = false}
                    class="px-3 py-1.5 text-body text-text-secondary hover:text-text transition-colors"
                >
                    Cancel
                </button>
            {/if}
        {/if}
    </div>

    {#if testResult}
        <div class="py-4">
            {#if testResult.ok}
                <div class="p-3 rounded-lg border-l-[3px] border-success bg-success/8">
                    <div class="flex items-center gap-2 mb-1">
                        <Icon name="check" size={14} class="text-success" />
                        <span class="text-body text-success">Connection successful</span>
                    </div>
                    {#if testResult.siteId}
                        <p class="text-caption text-text-secondary ml-[22px]">Site: {testResult.siteId}</p>
                    {/if}
                    {#if testResult.appVersion}
                        <p class="text-caption text-text-secondary ml-[22px]">Version: v{testResult.appVersion}</p>
                    {/if}
                </div>
            {:else}
                <div class="p-3 rounded-lg border-l-[3px] border-error bg-error/8">
                    <div class="flex items-center gap-2 mb-1">
                        <Icon name="x" size={14} class="text-error" />
                        <span class="text-body text-error">Connection failed</span>
                    </div>
                    {#if testResult.error}
                        <p class="text-caption text-error/80 ml-[22px]">{testResult.error}</p>
                    {/if}
                </div>
            {/if}
        </div>
    {/if}
</div>
