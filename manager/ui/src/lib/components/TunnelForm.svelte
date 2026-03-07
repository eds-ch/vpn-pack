<script>
    import { onMount } from 'svelte';
    import { SvelteSet } from 'svelte/reactivity';
    import { wgS2sGenerateKeypair, wgS2sGetLocalSubnets, wgS2sCreateTunnel, wgS2sListZones } from '../api.js';
    import { isValidCIDR, validateTunnelFields } from '../utils.js';
    import { WG_DEFAULT_PORT, WG_DEFAULT_MTU, WG_DEFAULT_KEEPALIVE } from '../constants.js';
    import { useClipboard } from '../helpers/clipboard.svelte.js';
    import { inputClass as getInputClass, clearFieldError } from '../helpers/field-errors.js';
    import FormField from './FormField.svelte';
    import Button from './Button.svelte';

    let { onCreated, onCancel, integrationConfigured = false } = $props();

    let keypair = $state(null);
    let localSubnets = $state([]);
    let selectedSubnets = $state(new SvelteSet());
    let customCIDRs = $state('');
    let loading = $state(false);
    let zones = $state([]);
    let selectedZone = $state('');
    let newZoneName = $state('WireGuard S2S');

    let name = $state('');
    let listenPort = $state(WG_DEFAULT_PORT);
    let tunnelAddress = $state('');
    let peerPublicKey = $state('');
    let peerEndpoint = $state('');
    let persistentKeepalive = $state(WG_DEFAULT_KEEPALIVE);
    let mtu = $state(WG_DEFAULT_MTU);
    const clip = useClipboard();

    let fieldErrors = $state({});

    onMount(async () => {
        const promises = [
            wgS2sGenerateKeypair(),
            wgS2sGetLocalSubnets(),
        ];
        if (integrationConfigured) promises.push(wgS2sListZones());

        const results = await Promise.all(promises);
        if (results[0]) keypair = results[0];
        if (Array.isArray(results[1])) localSubnets = results[1];
        if (results[2] && Array.isArray(results[2])) zones = results[2];
    });

    function toggleSubnet(cidr) {
        if (selectedSubnets.has(cidr)) {
            selectedSubnets.delete(cidr);
        } else {
            selectedSubnets.add(cidr);
        }
    }

    function parseWgConfig(text) {
        const result = {};
        for (const line of text.split('\n')) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith('#') || trimmed.startsWith('[')) continue;
            const [rawKey, ...valueParts] = trimmed.split('=');
            const key = rawKey.trim().toLowerCase();
            const value = valueParts.join('=').trim();
            if (key === 'publickey') result.publicKey = value;
            if (key === 'endpoint') result.endpoint = value;
            if (key === 'allowedips') result.allowedIPs = value.split(',').map(s => s.trim());
        }
        return result;
    }

    function handleSmartPaste(e) {
        const text = e.clipboardData?.getData('text') ?? '';
        if (!text.includes('\n')) return;
        e.preventDefault();
        const parsed = parseWgConfig(text);
        if (parsed.publicKey) peerPublicKey = parsed.publicKey;
        if (parsed.endpoint) peerEndpoint = parsed.endpoint;
        if (parsed.allowedIPs) {
            customCIDRs = parsed.allowedIPs.join(', ');
        }
    }

    function validate() {
        const errors = validateTunnelFields({
            name, listenPort, tunnelAddress, peerPublicKey,
            peerEndpoint, mtu, persistentKeepalive,
        });
        const customList = customCIDRs.split(',').map(s => s.trim()).filter(Boolean);
        for (const c of customList) {
            if (!isValidCIDR(c)) {
                errors.customCIDRs = `Invalid CIDR: ${c}`;
                break;
            }
        }
        return errors;
    }


    async function handleSubmit() {
        fieldErrors = validate();
        if (Object.keys(fieldErrors).length > 0) return;

        const customList = customCIDRs.split(',').map(s => s.trim()).filter(Boolean);

        loading = true;
        const payload = {
            name: name.trim(),
            listenPort: Number(listenPort),
            tunnelAddress: tunnelAddress.trim(),
            peerPublicKey: peerPublicKey.trim(),
            peerEndpoint: peerEndpoint.trim() || undefined,
            allowedIPs: customList,
            localSubnets: Array.from(selectedSubnets),
            persistentKeepalive: Number(persistentKeepalive),
            mtu: Number(mtu),
            privateKey: keypair?.privateKey || undefined,
        };
        if (integrationConfigured && zones.length > 0) {
            if (selectedZone === 'new') {
                payload.createZone = true;
                payload.zoneName = newZoneName.trim() || 'WireGuard S2S';
            } else {
                payload.zoneId = selectedZone || zones[0].zoneId;
            }
        }
        const result = await wgS2sCreateTunnel(payload);
        loading = false;

        if (result) {
            onCreated(result);
        }
    }
</script>

<div class="bg-surface rounded-xl border border-border p-4 md:p-5 space-y-4">
    <h3 class="text-body font-bold text-text-heading">New WireGuard Site-to-Site Tunnel</h3>

    {#if keypair?.publicKey}
        <div>
            <span class="text-caption text-text-secondary">Your Public Key (share with remote side)</span>
            <div class="mt-1 flex gap-2">
                <input
                    type="text"
                    readonly
                    value={keypair.publicKey}
                    class="flex-1 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text font-mono text-caption"
                />
                <Button variant="secondary" size="sm" onclick={() => clip.copy(keypair?.publicKey)}>{clip.copied ? 'Copied!' : clip.copyFailed ? 'Copy failed' : 'Copy'}</Button>
            </div>
        </div>
    {:else}
        <div class="h-10 bg-panel rounded-lg animate-pulse"></div>
    {/if}

    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div class="md:col-span-2">
            <FormField label="Tunnel Name" bind:value={name} placeholder="office-nyc"
                error={fieldErrors.name}
                oninput={() => fieldErrors = clearFieldError(fieldErrors,'name')} />
        </div>
        <div>
            <span class="text-caption text-text-secondary">Listen Port</span>
            <input type="number" bind:value={listenPort}
                oninput={() => fieldErrors = clearFieldError(fieldErrors,'listenPort')}
                class={getInputClass(fieldErrors,'listenPort')} />
            {#if fieldErrors.listenPort}<p class="text-caption text-error mt-0.5">{fieldErrors.listenPort}</p>{/if}
        </div>
        <FormField label="Tunnel Address (CIDR)" bind:value={tunnelAddress} placeholder="10.255.0.1/30"
            error={fieldErrors.tunnelAddress}
            oninput={() => fieldErrors = clearFieldError(fieldErrors,'tunnelAddress')} />
    </div>

    <FormField label="Remote Peer Public Key" bind:value={peerPublicKey}
        onpaste={handleSmartPaste}
        placeholder="Paste public key or full WireGuard config block"
        error={fieldErrors.peerPublicKey} extraClass="font-mono text-caption"
        oninput={() => fieldErrors = clearFieldError(fieldErrors,'peerPublicKey')} />

    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FormField label="Remote Peer Endpoint" bind:value={peerEndpoint} placeholder="85.12.34.56:51820"
            error={fieldErrors.peerEndpoint}
            oninput={() => fieldErrors = clearFieldError(fieldErrors,'peerEndpoint')} />
        <FormField label="Persistent Keepalive (s)" type="number" bind:value={persistentKeepalive}
            error={fieldErrors.persistentKeepalive}
            oninput={() => fieldErrors = clearFieldError(fieldErrors,'persistentKeepalive')} />
    </div>

    <FormField label="MTU" type="number" bind:value={mtu}
        error={fieldErrors.mtu} extraClass="!w-32"
        oninput={() => fieldErrors = clearFieldError(fieldErrors,'mtu')} />

    {#if integrationConfigured && zones.length > 0}
        <div>
            <span class="text-caption text-text-secondary font-bold uppercase tracking-wider">Firewall Zone</span>
            <div class="space-y-1.5 mt-2">
                {#each zones as zone (zone.zoneId)}
                    <label class="flex items-center gap-2 text-body cursor-pointer hover:bg-surface-hover rounded px-2 py-1.5 -mx-2 transition-colors">
                        <input
                            type="radio"
                            name="zone"
                            value={zone.zoneId}
                            checked={selectedZone === zone.zoneId || (selectedZone === '' && zones[0].zoneId === zone.zoneId)}
                            onchange={() => selectedZone = zone.zoneId}
                            class="w-4 h-4 accent-blue"
                        />
                        <span class="text-text">{zone.zoneName}</span>
                        <span class="text-caption text-text-secondary">({zone.tunnelCount} {zone.tunnelCount === 1 ? 'tunnel' : 'tunnels'})</span>
                    </label>
                {/each}
                <label class="flex items-center gap-2 text-body cursor-pointer hover:bg-surface-hover rounded px-2 py-1.5 -mx-2 transition-colors">
                    <input
                        type="radio"
                        name="zone"
                        value="new"
                        checked={selectedZone === 'new'}
                        onchange={() => selectedZone = 'new'}
                        class="w-4 h-4 accent-blue"
                    />
                    <span class="text-text">Create new zone</span>
                </label>
                {#if selectedZone === 'new'}
                    <input type="text" bind:value={newZoneName} placeholder="Zone name"
                        class="ml-6 w-64 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue" />
                {/if}
            </div>
        </div>
    {/if}

    {#if localSubnets.length > 0}
        <div>
            <span class="text-caption text-text-secondary font-bold uppercase tracking-wider">Local Subnets to Share</span>
            <div class="space-y-1 mt-2">
                {#each localSubnets as subnet (subnet.cidr)}
                    <label class="flex items-center gap-2 text-body cursor-pointer hover:bg-surface-hover rounded px-2 py-1 -mx-2 transition-colors">
                        <input
                            type="checkbox"
                            checked={selectedSubnets.has(subnet.cidr)}
                            onchange={() => toggleSubnet(subnet.cidr)}
                            class="w-4 h-4 rounded border-border text-blue accent-blue"
                        />
                        <span class="text-text-secondary">{subnet.name}</span>
                        <span class="text-text font-mono">{subnet.cidr}</span>
                    </label>
                {/each}
            </div>
        </div>
    {/if}

    <div>
        <span class="text-caption text-text-secondary">Custom Remote Subnets (comma-separated CIDRs)</span>
        <textarea bind:value={customCIDRs} rows="1" placeholder="10.20.0.0/24, 10.20.1.0/24"
            oninput={() => fieldErrors = clearFieldError(fieldErrors,'customCIDRs')}
            class="{getInputClass(fieldErrors,'customCIDRs')} font-mono resize-none"></textarea>
        {#if fieldErrors.customCIDRs}<p class="text-caption text-error mt-0.5">{fieldErrors.customCIDRs}</p>{/if}
    </div>

    <div class="flex gap-2 pt-1">
        <Button variant="primary" size="md" disabled={loading} onclick={handleSubmit}>{loading ? 'Creating...' : 'Create Tunnel'}</Button>
        <Button variant="secondary" size="md" onclick={onCancel}>Cancel</Button>
    </div>

</div>
