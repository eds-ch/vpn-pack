<script>
    import Toggle from './Toggle.svelte';

    let { value = false, activeVPNClients = [], dpiFingerprinting = null, onchange } = $props();

    let hasWgclt = $derived(activeVPNClients.length > 0);
    let wgcltNames = $derived(activeVPNClients.join(', '));
</script>

<div class="flex justify-between items-center py-4">
    <div class="flex-1 mr-4">
        <span class="text-body text-text">Advertise as Exit Node</span>
        <p class="text-caption text-text-tertiary mt-0.5">Route all traffic from other Tailscale nodes through this device.</p>
        {#if hasWgclt && value}
            <p class="text-caption text-warning mt-1">
                Advertising is safe, but don't route this device's own traffic through a remote exit node — Tailscale ip rules have higher priority and would override {wgcltNames} routing.
            </p>
        {/if}
        {#if dpiFingerprinting === false}
            <p class="text-caption text-warning mt-1">
                DPI fingerprinting is disabled while exit node is active to prevent system instability.
            </p>
        {/if}
    </div>
    <Toggle checked={value} onchange={() => onchange?.(!value)} />
</div>
