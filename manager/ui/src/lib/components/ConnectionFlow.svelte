<script>
    import QRCode from './QRCode.svelte';
    import AuthKeyInput from './AuthKeyInput.svelte';
    import { tailscaleUp } from '../api.js';

    let { status } = $props();

    const steps = [
        { id: 1, label: 'Starting tailscaled...' },
        { id: 2, label: 'Login required' },
        { id: 3, label: 'Connecting to tailnet...' },
        { id: 4, label: 'Connected' },
    ];

    let currentStep = $derived.by(() => {
        const state = status.backendState;
        if (state === 'Running') return 4;
        if (state === 'Starting') return 3;
        if (state === 'NeedsLogin') return 2;
        return 1;
    });

    function stepStatus(stepId) {
        if (stepId < currentStep) return 'completed';
        if (stepId === currentStep) return 'active';
        return 'pending';
    }

    let needsLogin = $derived(status.backendState === 'NeedsLogin');
    let loginTriggered = false;

    let integrationConfigured = $derived(status.integrationStatus?.configured ?? false);

    $effect(() => {
        if (needsLogin && !status.authURL && !loginTriggered && integrationConfigured) {
            loginTriggered = true;
            tailscaleUp();
        }
        if (!needsLogin) {
            loginTriggered = false;
        }
    });
</script>

<section class="bg-surface rounded-xl p-5 border border-border">
    <h3 class="text-caption font-bold text-text-secondary uppercase tracking-wider mb-4">Connection</h3>

    <div class="flex items-start gap-3 mb-4">
        {#each steps as step}
            {@const state = stepStatus(step.id)}
            <div class="flex flex-col items-center flex-1 min-w-0">
                <div class="w-7 h-7 rounded-full flex items-center justify-center text-caption font-bold shrink-0
                    {state === 'completed' ? 'bg-success text-white' : ''}
                    {state === 'active' ? 'bg-blue text-white' : ''}
                    {state === 'pending' ? 'bg-border text-text-secondary' : ''}
                ">
                    {#if state === 'completed'}
                        &#10003;
                    {:else}
                        {step.id}
                    {/if}
                </div>
                <span class="text-micro text-center mt-1 leading-tight
                    {state === 'active' ? 'text-text' : 'text-text-secondary'}
                ">{step.label}</span>
            </div>
            {#if step.id < steps.length}
                <div class="w-8 h-0.5 mt-3.5 shrink-0
                    {step.id < currentStep ? 'bg-success' : 'bg-border'}
                "></div>
            {/if}
        {/each}
    </div>

    {#if needsLogin}
        <div class="border-t border-border pt-4 mt-2">
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div class="p-4 rounded-lg border border-border bg-panel">
                    <h4 class="text-body font-bold text-text-heading mb-3">Login with browser</h4>
                    {#if status.authURL}
                        <p class="text-caption text-text-secondary mb-2">
                            Open this URL or scan the QR code:
                        </p>
                        <a
                            href={status.authURL}
                            target="_blank"
                            rel="noopener noreferrer"
                            class="text-body text-blue hover:text-blue-hover break-all"
                        >
                            {status.authURL}
                        </a>
                        <div class="mt-3 flex justify-center">
                            <QRCode value={status.authURL} />
                        </div>
                    {:else}
                        <p class="text-caption text-text-secondary animate-pulse">Waiting for auth URL...</p>
                    {/if}
                </div>

                <div class="p-4 rounded-lg border border-border bg-panel">
                    <h4 class="text-body font-bold text-text-heading mb-3">Connect with auth key</h4>
                    <p class="text-caption text-text-secondary mb-3">
                        Use a pre-authentication key from the Tailscale admin console.
                    </p>
                    <AuthKeyInput />
                </div>
            </div>
        </div>
    {/if}
</section>
