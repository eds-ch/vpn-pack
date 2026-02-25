<script>
    let { checked = false, onchange } = $props();
</script>

<label class="relative inline-flex h-5 w-9 items-center rounded-full cursor-pointer shrink-0 {checked ? 'bg-blue' : 'bg-border'}">
    <input type="checkbox" class="sr-only" {checked} {onchange} />
    <span class="inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform {checked ? 'translate-x-5' : 'translate-x-0.5'}"></span>
</label>
