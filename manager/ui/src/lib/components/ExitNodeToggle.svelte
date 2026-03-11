<script>
    import Toggle from './Toggle.svelte';
    import Button from './Button.svelte';

    let { value = false, activeVPNClients = [], dpiFingerprinting = null, onchange } = $props();

    let showConfirm = $state(false);

    let hasWgclt = $derived(activeVPNClients.length > 0);
    let wgcltNames = $derived(activeVPNClients.join(', '));

    function handleToggle() {
        if (!value) {
            showConfirm = true;
        } else {
            onchange?.(false);
        }
    }

    function confirmEnable() {
        showConfirm = false;
        onchange?.(true);
    }

    function cancelConfirm() {
        showConfirm = false;
    }
</script>

<div class="py-4">
    <div class="flex justify-between items-center">
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
        <Toggle checked={value} onchange={handleToggle} />
    </div>

    {#if showConfirm}
        <div class="mt-3 p-3 rounded-lg bg-warning/10 border border-warning/30">
            <p class="text-body text-warning font-bold mb-1">Confirm exit node</p>
            <p class="text-caption text-text-secondary mb-3">
                ALL internet traffic from ALL clients behind this router
                (all VLANs, all devices) will be routed through the Tailscale
                exit node. Direct internet access will be lost.
            </p>
            <p class="text-caption text-text-tertiary mb-3">
                For per-device routing, use Tailscale ACLs on individual clients.
            </p>
            <div class="flex gap-2">
                <Button variant="warning" size="sm" onclick={confirmEnable}>Enable Exit Node</Button>
                <Button variant="secondary" size="sm" onclick={cancelConfirm}>Cancel</Button>
            </div>
        </div>
    {/if}
</div>
