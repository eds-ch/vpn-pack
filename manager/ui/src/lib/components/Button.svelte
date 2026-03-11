<script>
    let {
        variant = 'primary',
        size = 'md',
        disabled = false,
        onclick = undefined,
        children,
    } = $props();

    const classes = $derived.by(() => {
        const base = 'rounded-lg text-body transition-colors disabled:opacity-50 disabled:cursor-not-allowed';
        if (variant === 'primary') {
            const pad = size === 'sm' ? 'px-4 py-1.5' : 'px-6 py-2';
            return `${pad} ${base} font-bold bg-blue text-white hover:bg-blue-hover`;
        }
        if (variant === 'warning') {
            const pad = size === 'sm' ? 'px-3 py-1.5' : 'px-6 py-2';
            return `${pad} ${base} font-bold bg-warning text-white hover:bg-warning/80`;
        }
        if (variant === 'secondary') {
            const sz = size === 'sm' ? 'px-3 py-1.5' : 'px-4 py-1.5 font-bold';
            return `${sz} ${base} border border-border text-text hover:bg-surface-hover`;
        }
        return `px-3 py-1.5 ${base} text-text-secondary hover:text-text`;
    });
</script>

<button type="button" class={classes} {disabled} {onclick}>
    {@render children()}
</button>
