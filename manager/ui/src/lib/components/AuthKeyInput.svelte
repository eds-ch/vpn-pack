<script>
    import Icon from './Icon.svelte';
    import { connectWithAuthKey } from '../api.js';

    let authKey = $state('');
    let loading = $state(false);
    let error = $state('');
    let revealed = $state(false);

    async function handleSubmit() {
        const trimmed = authKey.trim();
        if (!trimmed) return;

        if (!trimmed.startsWith('tskey-')) {
            error = 'Auth key must start with "tskey-" prefix';
            return;
        }

        error = '';
        loading = true;
        const result = await connectWithAuthKey(trimmed);
        loading = false;

        if (result?.ok) {
            authKey = '';
        } else if (!result) {
            error = 'Connection failed. Check the error panel for details.';
        }
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') handleSubmit();
    }
</script>

<div class="space-y-3">
    <div class="relative">
        <input
            type={revealed ? 'text' : 'password'}
            bind:value={authKey}
            onkeydown={handleKeydown}
            disabled={loading}
            placeholder="tskey-auth-..."
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"
            spellcheck="false"
            class="w-full pl-3 pr-10 py-2 text-body rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue disabled:opacity-50"
        />
        <button
            type="button"
            onclick={() => (revealed = !revealed)}
            disabled={loading}
            aria-label={revealed ? 'Hide auth key' : 'Show auth key'}
            aria-pressed={revealed}
            class="absolute inset-y-0 right-0 px-2.5 flex items-center text-text-secondary hover:text-text disabled:opacity-50 transition-colors"
        >
            <Icon name={revealed ? 'eye-off' : 'eye'} size={16} />
        </button>
    </div>
    {#if error}
        <p class="text-caption text-error">{error}</p>
    {/if}
    <button
        onclick={handleSubmit}
        disabled={loading || !authKey.trim()}
        class="w-full px-4 py-2 rounded-lg text-body font-bold bg-blue text-white hover:bg-blue-hover disabled:opacity-50 transition-colors flex items-center justify-center gap-2"
    >
        {#if loading}
            <span class="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
            Connecting...
        {:else}
            Connect
        {/if}
    </button>
</div>
