<script>
    import Icon from './Icon.svelte';

    let { value = $bindable(''), disabled = false, onEnter = null, id = undefined } = $props();
    let showKey = $state(false);
</script>

<div class="space-y-2">
    <div class="relative">
        <input
            {id}
            type={showKey ? 'text' : 'password'}
            bind:value
            onkeydown={(e) => { if (e.key === 'Enter') onEnter?.(); }}
            placeholder="Enter UniFi API key"
            {disabled}
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
