<script>
    import Toggle from './Toggle.svelte';
    import { isValidEndpoint } from '../utils.js';
    import { TS_DEFAULT_UDP_PORT, TS_DEFAULT_RELAY_PORT, PORT_MIN, PORT_MAX, RELAY_PORT_MIN, HOSTNAME_MAX_LENGTH } from '../constants.js';

    let { staged, original, stageChange, onValidation = () => {} } = $props();

    let relayEnabled = $derived(staged.relayServerPort != null && staged.relayServerPort >= 0);

    let hostnameError = $derived.by(() => {
        const v = staged.hostname;
        if (v == null || v === '') return '';
        if (v.length > HOSTNAME_MAX_LENGTH) return `Maximum ${HOSTNAME_MAX_LENGTH} characters`;
        if (!/^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$/.test(v)) return 'Letters, numbers, and hyphens only';
        return '';
    });

    let udpPortError = $derived.by(() => {
        const v = staged.udpPort;
        if (v == null) return '';
        const n = Number(v);
        if (!Number.isInteger(n) || n < PORT_MIN || n > PORT_MAX) return `Must be ${PORT_MIN}-${PORT_MAX}`;
        return '';
    });

    let relayPortError = $derived.by(() => {
        const v = staged.relayServerPort;
        if (v == null || v < 0) return '';
        const n = Number(v);
        if (!Number.isInteger(n) || n < RELAY_PORT_MIN || n > PORT_MAX) return `Must be ${RELAY_PORT_MIN}-${PORT_MAX}`;
        return '';
    });

    let relayEndpointsError = $derived.by(() => {
        const v = staged.relayServerEndpoints;
        if (!v?.trim()) return '';
        const parts = v.split(',').map(s => s.trim()).filter(Boolean);
        for (const p of parts) {
            if (!isValidEndpoint(p)) return `Invalid endpoint: ${p} (must be IP:port)`;
        }
        return '';
    });

    let hasErrors = $derived(!!hostnameError || !!udpPortError || !!relayPortError || !!relayEndpointsError);

    $effect(() => { onValidation(hasErrors); });

    function toggleRelay(checked) {
        if (checked) {
            stageChange('relayServerPort', staged.relayServerPort > 0 ? staged.relayServerPort : TS_DEFAULT_RELAY_PORT);
        } else {
            stageChange('relayServerPort', -1);
        }
    }
</script>

<div>
    <h2 class="text-heading text-text-heading">General</h2>
    <p class="text-caption text-text-tertiary mt-1">Core Tailscale settings for this device</p>
</div>

<div class="divide-y divide-border mt-8">
    <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2 py-4 first:pt-0">
        <div>
            <label for="hostname" class="text-body text-text">Device Hostname</label>
            <p class="text-caption text-text-tertiary mt-0.5">Name of this device in your tailnet. Visible to other Tailscale nodes.</p>
            {#if hostnameError}
                <p class="text-caption text-error mt-0.5">{hostnameError}</p>
            {/if}
        </div>
        <input
            id="hostname"
            type="text"
            value={staged.hostname ?? ''}
            placeholder={original.hostname ?? 'hostname'}
            oninput={(e) => stageChange('hostname', e.target.value)}
            class="w-full sm:w-64 px-3 py-1.5 text-body rounded-lg border bg-input text-text placeholder-text-secondary focus:outline-none
                {hostnameError ? 'border-error' : 'border-border focus:border-blue'}"
        />
    </div>

    <div class="flex justify-between items-center py-4">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Accept Routes</span>
            <p class="text-caption text-text-tertiary mt-0.5">Accept subnet routes advertised by other nodes in your tailnet.</p>
            {#if staged.acceptRoutes}
                <p class="text-caption text-warning mt-1">Enabling accept-routes may conflict with UniFi routing tables. MagicDNS is always disabled on UniFi devices.</p>
            {/if}
        </div>
        <Toggle checked={staged.acceptRoutes ?? false} onchange={(e) => stageChange('acceptRoutes', e.target.checked)} />
    </div>

    <div class="flex justify-between items-center py-4">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Shields Up</span>
            <p class="text-caption text-text-tertiary mt-0.5">Block all incoming connections over Tailscale to this device.</p>
            {#if staged.shieldsUp}
                <p class="text-caption text-warning mt-1">Shields Up blocks all incoming connections over Tailscale. This device will not be reachable from the tailnet.</p>
            {/if}
        </div>
        <Toggle checked={staged.shieldsUp ?? false} onchange={(e) => stageChange('shieldsUp', e.target.checked)} />
    </div>

    <div class="flex justify-between items-center py-4">
        <div class="flex-1 mr-4">
            <span class="text-body text-text">Tailscale SSH</span>
            <p class="text-caption text-text-tertiary mt-0.5">Allow SSH connections over Tailscale to this device.</p>
        </div>
        <Toggle checked={staged.runSSH ?? false} onchange={(e) => stageChange('runSSH', e.target.checked)} />
    </div>

    <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2 py-4">
        <div>
            <label for="udpPort" class="text-body text-text">WireGuard UDP Port</label>
            <p class="text-caption text-text-tertiary mt-0.5">Changing the port requires restarting tailscaled. Current connections will be briefly interrupted.</p>
            {#if udpPortError}
                <p class="text-caption text-error mt-0.5">{udpPortError}</p>
            {/if}
        </div>
        <input
            id="udpPort"
            type="number"
            value={staged.udpPort ?? TS_DEFAULT_UDP_PORT}
            oninput={(e) => stageChange('udpPort', parseInt(e.target.value) || TS_DEFAULT_UDP_PORT)}
            class="w-full sm:w-32 px-3 py-1.5 text-body rounded-lg border bg-input text-text focus:outline-none
                {udpPortError ? 'border-error' : 'border-border focus:border-blue'}"
        />
    </div>

    <div class="py-4">
        <div class="flex justify-between items-center">
            <div class="flex-1 mr-4">
                <span class="text-body text-text">Peer Relay Server</span>
                <p class="text-caption text-text-tertiary mt-0.5">Allow this device to relay traffic for other tailnet nodes that cannot establish direct connections.</p>
            </div>
            <Toggle checked={relayEnabled} onchange={(e) => toggleRelay(e.target.checked)} />
        </div>

        {#if relayEnabled}
            <div class="mt-3 space-y-3">
                <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2">
                    <div>
                        <label for="relayPort" class="text-body text-text">Relay UDP Port</label>
                        <p class="text-caption text-text-tertiary mt-0.5">UDP port for relay connections. Must be accessible from the internet.</p>
                        {#if relayPortError}
                            <p class="text-caption text-error mt-0.5">{relayPortError}</p>
                        {/if}
                    </div>
                    <input
                        id="relayPort"
                        type="number"
                        min="1024"
                        max="65535"
                        value={staged.relayServerPort ?? TS_DEFAULT_RELAY_PORT}
                        oninput={(e) => stageChange('relayServerPort', parseInt(e.target.value) || TS_DEFAULT_RELAY_PORT)}
                        class="w-full sm:w-32 px-3 py-1.5 text-body rounded-lg border bg-input text-text focus:outline-none
                            {relayPortError ? 'border-error' : 'border-border focus:border-blue'}"
                    />
                </div>

                <div class="flex flex-col sm:flex-row sm:justify-between sm:items-center gap-2">
                    <div>
                        <label for="relayEndpoints" class="text-body text-text">Static Endpoints</label>
                        <p class="text-caption text-text-tertiary mt-0.5">Optional. Static IP:port endpoints if this device is behind port forwarding.</p>
                        {#if relayEndpointsError}
                            <p class="text-caption text-error mt-0.5">{relayEndpointsError}</p>
                        {/if}
                    </div>
                    <input
                        id="relayEndpoints"
                        type="text"
                        value={staged.relayServerEndpoints ?? ''}
                        placeholder="203.0.113.1:40000"
                        oninput={(e) => stageChange('relayServerEndpoints', e.target.value)}
                        class="w-full sm:w-64 px-3 py-1.5 text-body rounded-lg border bg-input text-text placeholder-text-secondary focus:outline-none
                            {relayEndpointsError ? 'border-error' : 'border-border focus:border-blue'}"
                    />
                </div>

                <div class="rounded-lg border border-blue/20 bg-blue/5 px-4 py-3 text-caption text-text-secondary">
                    <div class="flex gap-2">
                        <svg class="w-4 h-4 text-blue shrink-0 mt-0.5" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z" clip-rule="evenodd" />
                        </svg>
                        <div>
                            <p><span class="text-text">ACL Policy Required:</span> Peer relay requires a grant in your Tailscale ACL policy with the <code class="px-1 py-0.5 rounded bg-surface text-text font-mono text-caption">tailscale.com/cap/relay</code> capability for this device. <a href="https://tailscale.com/kb/1450/peer-relay" target="_blank" rel="noopener noreferrer" class="text-blue hover:underline">Documentation</a></p>
                        </div>
                    </div>
                </div>
            </div>
        {/if}
    </div>
</div>
