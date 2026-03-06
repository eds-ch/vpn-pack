<script>
    import { onMount } from 'svelte';
    import { getSubnets } from '../api.js';
    import { isValidCIDR } from '../utils.js';
    import Icon from './Icon.svelte';

    let { value = [], routes = [], onchange } = $props();

    let detectedSubnets = $state([]);
    let customCIDR = $state('');
    let loading = $state(false);
    let cidrError = $state('');

    let approvalMap = $derived(new Map(routes.map(r => [r.cidr, r.approved])));
    let hasPending = $derived(routes.some(r => !r.approved));
    let selectedSet = $derived(new Set(value));
    let detectedCIDRSet = $derived(new Set(detectedSubnets.map(s => s.cidr)));
    let customRoutes = $derived(value.filter(c => !detectedCIDRSet.has(c)));

    onMount(async () => {
        loading = true;
        const subnetsData = await getSubnets();
        if (subnetsData) {
            detectedSubnets = subnetsData.subnets || [];
        }
        loading = false;
    });

    function approvalBadge(cidr) {
        if (!approvalMap.has(cidr)) return null;
        return approvalMap.get(cidr) ? 'approved' : 'pending';
    }

    function toggleRoute(cidr) {
        const newValue = selectedSet.has(cidr)
            ? value.filter(c => c !== cidr)
            : [...value, cidr];
        onchange?.(newValue);
    }

    function addCustom() {
        const trimmed = customCIDR.trim();
        if (!trimmed) return;
        if (!isValidCIDR(trimmed)) {
            cidrError = 'Invalid CIDR format (e.g. 10.0.0.0/24)';
            return;
        }
        if (selectedSet.has(trimmed)) {
            cidrError = 'Route already exists';
            return;
        }
        cidrError = '';
        onchange?.([...value, trimmed]);
        customCIDR = '';
    }

    function removeCustom(cidr) {
        onchange?.(value.filter(c => c !== cidr));
    }

    function handleKeydown(e) {
        if (e.key === 'Enter') addCustom();
    }
</script>

<div class="py-4 first:pt-0">
    <span class="text-body text-text">Subnet Routes</span>
    <p class="text-caption text-text-tertiary mt-0.5">Advertise local network subnets to your Tailscale network</p>

    {#if loading}
        <div class="mt-3 space-y-2">
            {#each [1, 2] as _, i (i)}
                <div class="h-8 bg-surface rounded-lg animate-pulse"></div>
            {/each}
        </div>
    {:else}
        {#if detectedSubnets.length > 0}
            <div class="mt-3 space-y-0.5">
                {#each detectedSubnets as subnet (subnet.cidr)}
                    <label class="flex items-center gap-2.5 text-body cursor-pointer hover:bg-surface-hover rounded-lg px-2 py-1.5 -mx-2 transition-colors">
                        <input
                            type="checkbox"
                            checked={selectedSet.has(subnet.cidr)}
                            onchange={() => toggleRoute(subnet.cidr)}
                            class="w-4 h-4 rounded border-border text-blue accent-blue"
                        />
                        <span class="text-text font-mono text-caption">{subnet.cidr}</span>
                        <span class="text-text-secondary text-caption">{subnet.name}</span>
                        {#if approvalBadge(subnet.cidr) === 'approved'}
                            <span class="ml-auto text-caption text-success">Approved</span>
                        {:else if approvalBadge(subnet.cidr) === 'pending'}
                            <span class="ml-auto text-caption text-warning">Pending approval</span>
                        {/if}
                    </label>
                {/each}
            </div>
        {:else}
            <p class="text-caption text-text-secondary mt-3">No subnets detected. Add routes manually below.</p>
        {/if}

        {#if customRoutes.length > 0}
            <div class="mt-2 space-y-0.5">
                <span class="text-caption text-text-tertiary px-2">Custom</span>
                {#each customRoutes as cidr (cidr)}
                    <div class="flex items-center gap-2.5 text-body px-2 py-1.5 -mx-2">
                        <input
                            type="checkbox"
                            checked={selectedSet.has(cidr)}
                            onchange={() => toggleRoute(cidr)}
                            class="w-4 h-4 rounded border-border text-blue accent-blue"
                        />
                        <span class="text-text font-mono text-caption">{cidr}</span>
                        {#if approvalBadge(cidr) === 'approved'}
                            <span class="text-caption text-success">Approved</span>
                        {:else if approvalBadge(cidr) === 'pending'}
                            <span class="text-caption text-warning">Pending approval</span>
                        {/if}
                        <button
                            onclick={() => removeCustom(cidr)}
                            class="ml-auto text-text-tertiary hover:text-error text-caption transition-colors"
                        >&times;</button>
                    </div>
                {/each}
            </div>
        {/if}

        <div class="flex gap-2 mt-3">
            <input
                type="text"
                bind:value={customCIDR}
                onkeydown={handleKeydown}
                placeholder="10.0.0.0/24"
                class="flex-1 px-3 py-1.5 text-body rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue"
            />
            <button
                onclick={addCustom}
                class="px-3 py-1.5 text-body rounded-lg border border-border text-text hover:bg-surface-hover transition-colors"
            >Add</button>
        </div>
        {#if cidrError}
            <p class="text-caption text-error mt-1.5">{cidrError}</p>
        {/if}

        {#if hasPending}
            <div class="mt-3 p-3 rounded-lg bg-warning/10 border border-warning/30 flex gap-2">
                <Icon name="alert-triangle" size={16} class="text-warning shrink-0 mt-0.5" />
                <div>
                    <p class="text-body text-warning">Some routes are pending approval in the Tailscale admin console.</p>
                    <a
                        href="https://login.tailscale.com/admin/machines"
                        target="_blank"
                        rel="noopener noreferrer"
                        class="text-body text-blue hover:text-blue-hover mt-1 inline-block"
                    >Open Tailscale Admin Console</a>
                </div>
            </div>
        {/if}
    {/if}
</div>
