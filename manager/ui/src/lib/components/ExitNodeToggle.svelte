<script>
    import Toggle from './Toggle.svelte';

    let {
        enabled = false,
        activeVPNClients = [],
        dpiFingerprinting = null,
        onchange,
    } = $props();

    let hasWgclt = $derived(activeVPNClients.length > 0);
    let wgcltNames = $derived(activeVPNClients.join(', '));

    function handleToggle() {
        onchange?.(!enabled);
    }
</script>

<div class="py-4">
    <div class="flex justify-between items-center">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Advertise as Exit Node</span>
            <p class="text-caption text-text-tertiary mt-0.5">Make this router available as an exit node for other Tailscale devices. Your LAN clients are not affected.</p>
            {#if hasWgclt && enabled}
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
        <Toggle checked={enabled} onchange={handleToggle} />
    </div>
</div>
