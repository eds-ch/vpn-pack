<script>
    import Icon from './Icon.svelte';
    import { formatUptime } from '../utils.js';

    let { deviceInfo } = $props();
</script>

<section class="bg-surface rounded-xl border border-border overflow-hidden">
    <div class="p-4 flex flex-col">
        <div class="flex items-center justify-between mb-3">
            <h3 class="flex items-center gap-1.5 text-caption font-bold text-text-secondary uppercase tracking-wider">
                <Icon name="ubiquiti" size={14} class="shrink-0" /><span class="cap-center">UniFi</span>
            </h3>
            {#if deviceInfo?.modelShort || deviceInfo?.model}
                <span class="text-caption font-bold text-text-heading">{deviceInfo.modelShort || deviceInfo.model}</span>
            {/if}
        </div>

        <div class="flex flex-col gap-1.5 text-body">
            {#if deviceInfo?.hostname}
                <div class="flex justify-between">
                    <span class="text-text-secondary shrink-0">Hostname</span>
                    <span class="text-text truncate ml-4">{deviceInfo.hostname}</span>
                </div>
            {/if}

            {#if deviceInfo?.firmware}
                <div class="flex justify-between">
                    <span class="text-text-secondary shrink-0">Firmware</span>
                    <span class="text-text">{deviceInfo.firmware}</span>
                </div>
            {/if}

            {#if deviceInfo?.unifiVersion}
                <div class="flex justify-between">
                    <span class="text-text-secondary shrink-0">UniFi Network</span>
                    <span class="text-text">{deviceInfo.unifiVersion}</span>
                </div>
            {/if}

            <div class="flex justify-between">
                <span class="text-text-secondary shrink-0">Uptime</span>
                <span class="text-text">{formatUptime(deviceInfo?.uptime)}</span>
            </div>
        </div>
    </div>
</section>
