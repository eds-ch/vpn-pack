<script>
    let { deviceInfo } = $props();
</script>

<section class="bg-surface rounded-xl p-5 border border-border">
    <h3 class="text-caption font-bold text-text-secondary uppercase tracking-wider mb-3">Device</h3>

    {#if deviceInfo}
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2 text-body">
            <div class="flex justify-between sm:justify-start sm:gap-3">
                <span class="text-text-secondary">Model</span>
                <span class="text-text">{deviceInfo.model ?? 'N/A'}</span>
            </div>
            <div class="flex justify-between sm:justify-start sm:gap-3">
                <span class="text-text-secondary">Firmware</span>
                <span class="text-text">{deviceInfo.firmware ?? 'N/A'}</span>
            </div>
            <div class="flex justify-between sm:justify-start sm:gap-3">
                <span class="text-text-secondary">UniFi Network</span>
                <span class="text-text">{deviceInfo.unifiVersion ?? 'N/A'}</span>
            </div>
            <div class="flex justify-between sm:justify-start sm:gap-3">
                <span class="text-text-secondary">TUN</span>
                <span class="text-text {deviceInfo.hasTUN ? 'text-success' : 'text-error'}">
                    {deviceInfo.hasTUN ? 'Available' : 'Unavailable'}
                </span>
            </div>
            <div class="flex justify-between sm:justify-start sm:gap-3">
                <span class="text-text-secondary">UDAPI Socket</span>
                <span class="text-text {deviceInfo.hasUDAPISocket ? 'text-success' : 'text-error'}">
                    {deviceInfo.hasUDAPISocket ? 'Connected' : 'Unavailable'}
                </span>
            </div>
            {#if deviceInfo.persistentFree}
                <div class="flex justify-between sm:justify-start sm:gap-3">
                    <span class="text-text-secondary">Persistent Storage</span>
                    <span class="text-text">{deviceInfo.persistentFree}</span>
                </div>
            {/if}
        </div>
    {:else}
        <p class="text-body text-text-secondary animate-pulse">Detecting device...</p>
    {/if}
</section>
