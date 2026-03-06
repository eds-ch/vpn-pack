<script>
    let {
        variant = 'primary',
        size = 'md',
        disabled = false,
        onclick = undefined,
        children,
    } = $props();

    const classes = $derived.by(() => {
        const base = 'rounded-lg text-body transition-colors';
        if (variant === 'primary') {
            const pad = size === 'sm' ? 'px-4 py-1.5' : 'px-6 py-2';
            const state = disabled ? 'opacity-50 cursor-not-allowed' : 'hover:bg-blue-hover';
            return `${pad} ${base} font-bold bg-blue text-white ${state}`;
        }
        if (variant === 'secondary') {
            const pad = size === 'sm' ? 'px-3 py-1.5' : 'px-4 py-1.5 font-bold';
            return `${pad} ${base} border border-border text-text hover:bg-surface-hover disabled:opacity-50`;
        }
        // ghost
        const pad = size === 'sm' ? 'px-3 py-1.5' : 'px-3 py-1.5';
        return `${pad} ${base} text-text-secondary hover:text-text`;
    });
</script>

<button class={classes} {disabled} {onclick}>
    {@render children()}
</button>
