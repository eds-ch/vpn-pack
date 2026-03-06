<script>
    let {
        label,
        type = 'text',
        value = $bindable(),
        error = undefined,
        placeholder = undefined,
        extraClass = '',
        oninput = undefined,
        onpaste = undefined,
    } = $props();

    const BASE = 'mt-1 w-full px-3 py-1.5 text-body rounded-lg bg-input text-text placeholder-text-secondary focus:outline-none transition-colors';
    const ERROR_CLS = `${BASE} border-2 border-error focus:border-error`;
    const NORMAL_CLS = `${BASE} border border-border focus:border-blue`;

    let inputCls = $derived((error ? ERROR_CLS : NORMAL_CLS) + (extraClass ? ` ${extraClass}` : ''));
</script>

<div>
    <span class="text-caption text-text-secondary">{label}</span>
    <input {type} bind:value {placeholder} {oninput} {onpaste} class={inputCls} />
    {#if error}<p class="text-caption text-error mt-0.5">{error}</p>{/if}
</div>
