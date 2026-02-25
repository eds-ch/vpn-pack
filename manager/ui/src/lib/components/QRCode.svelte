<script>
    import QRCodeLib from 'qrcode';

    let { value = '' } = $props();
    let canvas = $state(null);

    $effect(() => {
        if (canvas && value) {
            QRCodeLib.toCanvas(canvas, value, {
                width: 200,
                margin: 2,
                color: {
                    dark: '#1a1a2e',
                    light: '#ffffff',
                },
            });
        }
    });
</script>

{#if value}
    <div class="inline-block rounded-lg overflow-hidden bg-white p-1">
        <canvas bind:this={canvas} width="200" height="200" aria-label="QR code for authentication URL"></canvas>
    </div>
{/if}
