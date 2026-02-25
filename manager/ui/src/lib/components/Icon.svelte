<script>
    import { icons } from '../icons.js';
    let { name, size = 20, class: className = '' } = $props();
</script>

{#if icons[name]}
    <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        class={className}
    >
        <!-- Safe: icons[name] contains only hardcoded SVG path fragments from icons.js, never user input -->
        {@html icons[name]}
    </svg>
{/if}
