<script>
    let { value = '' } = $props();
</script>

{#if value}
    <div data-testid="qrcode-mock">{value}</div>
{/if}
