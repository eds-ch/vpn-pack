<script>
    import Toggle from './Toggle.svelte';

    let { staged, original, stageChange, onValidation = () => {} } = $props();

    let controlURLChanged = $derived(
        staged.controlURL !== original.controlURL
    );

    let controlURLError = $derived.by(() => {
        const url = staged.controlURL;
        if (!url) return '';
        try {
            const u = new URL(url);
            if (u.protocol !== 'https:') return 'URL must use HTTPS';
            if (!u.hostname) return 'URL must have a hostname';
        } catch {
            return 'Invalid URL format';
        }
        return '';
    });

    $effect(() => { onValidation(!!controlURLError); });
</script>

<div>
    <h2 class="text-heading text-text-heading">Advanced</h2>
    <p class="text-caption text-text-tertiary mt-1">Control server and network-level options</p>
</div>

<div class="divide-y divide-border mt-8">
    <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2 py-4 first:pt-0">
        <div class="flex-1 mr-4">
            <label for="controlURL" class="text-body text-text">Control Server URL</label>
            <p class="text-caption text-text-tertiary mt-0.5">Coordination server address. Change only for self-hosted Headscale or custom control planes.</p>
            {#if controlURLChanged}
                <p class="text-caption text-error mt-1">Changing the control server will disconnect from the current tailnet. You must log out first.</p>
            {/if}
            {#if controlURLError}
                <p class="text-caption text-error mt-1">{controlURLError}</p>
            {/if}
        </div>
        <input
            id="controlURL"
            type="text"
            value={staged.controlURL ?? ''}
            placeholder="https://controlplane.tailscale.com (default)"
            oninput={(e) => stageChange('controlURL', e.target.value)}
            class="w-full sm:w-80 px-3 py-1.5 text-body rounded-lg border bg-input text-text placeholder-text-secondary focus:outline-none
                {controlURLError ? 'border-error' : 'border-border focus:border-blue'}"
        />
    </div>

    <div class="flex justify-between items-center py-4">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Tailscale DNS</span>
            <p class="text-caption text-text-tertiary mt-0.5">Use Tailscale's DNS configuration (MagicDNS) on this device.</p>
            {#if staged.acceptDNS}
                <p class="text-caption text-warning mt-1">Tailscale will overwrite this device's DNS configuration (resolv.conf). All DNS queries from LAN clients using this router as their DNS server will be routed through Tailscale. If tailscaled stops, DNS resolution will break until the device is rebooted.</p>
            {/if}
        </div>
        <Toggle checked={staged.acceptDNS ?? false} onchange={(e) => stageChange('acceptDNS', e.target.checked)} />
    </div>

    <div class="flex justify-between items-center py-4">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Disable Source NAT</span>
            <p class="text-caption text-text-tertiary mt-0.5">Disable SNAT for subnet routes. Required for bidirectional access from remote subnets.</p>
            {#if staged.noSNAT}
                <p class="text-caption text-warning mt-1">Remote clients will see the real source IP instead of the Tailscale IP.</p>
            {/if}
        </div>
        <Toggle checked={staged.noSNAT ?? false} onchange={(e) => stageChange('noSNAT', e.target.checked)} />
    </div>
</div>
