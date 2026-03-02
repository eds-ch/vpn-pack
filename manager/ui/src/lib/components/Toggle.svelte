<script>
    let { checked = false, disabled = false, onchange } = $props();
</script>

<label class="relative inline-flex h-5 w-9 items-center rounded-full shrink-0 {disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'} {checked ? 'bg-blue' : 'bg-border'}">
    <input type="checkbox" class="sr-only" {checked} {disabled} {onchange} />
    <span class="inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform {checked ? 'translate-x-5' : 'translate-x-0.5'}"></span>
</label>
