<script>
    import { fetchLogs } from '../api.js';
    import { LOG_POLL_INTERVAL_MS } from '../constants.js';

    let daemonLogs = $state([]);
    let filter = $state('all');
    let source = $state('all');
    let search = $state('');
    let autoScroll = $state(true);
    let container = $state(null);
    let lastUpdated = $state(null);

    const levelFilters = [
        { id: 'all', label: 'All' },
        { id: 'info', label: 'Info' },
        { id: 'warn', label: 'Warn' },
        { id: 'error', label: 'Error' },
    ];

    const sourceFilters = [
        { id: 'all', label: 'All' },
        { id: 'tailscale', label: 'Tailscale' },
        { id: 'wgs2s', label: 'WG S2S' },
    ];

    let filtered = $derived.by(() => {
        let result = daemonLogs;
        if (source !== 'all') {
            result = result.filter(l =>
                source === 'tailscale' ? !l.source : l.source === 'wgs2s'
            );
        }
        if (filter !== 'all') {
            result = result.filter(l => l.level === filter);
        }
        if (search) {
            const q = search.toLowerCase();
            result = result.filter(l => l.message.toLowerCase().includes(q));
        }
        return result;
    });

    const badgeColors = {
        info: 'bg-info/20 text-info',
        warn: 'bg-warning/20 text-warning',
        error: 'bg-error/20 text-error',
    };

    async function loadLogs() {
        const data = await fetchLogs();
        if (data?.lines) {
            daemonLogs = data.lines;
        }
        lastUpdated = new Date();
    }

    function handleScroll() {
        if (!container) return;
        autoScroll = container.scrollTop <= 10;
    }

    $effect(() => {
        if (autoScroll && container && filtered.length > 0) {
            container.scrollTop = 0;
        }
    });

    $effect(() => {
        loadLogs();
        const timer = setInterval(loadLogs, LOG_POLL_INTERVAL_MS);
        return () => clearInterval(timer);
    });
</script>

<div class="flex flex-col flex-1 min-h-0 gap-2 md:gap-3">
    <div class="flex flex-wrap items-center gap-2 md:gap-3 shrink-0">
        <div class="flex rounded-lg border border-border overflow-hidden">
            {#each sourceFilters as f (f.id)}
                <button
                    class="h-8 px-3 text-caption font-bold transition-colors
                        {source === f.id
                            ? 'bg-blue text-white'
                            : 'text-text-secondary hover:text-text hover:bg-surface'}"
                    onclick={() => source = f.id}
                >
                    {f.label}
                </button>
            {/each}
        </div>

        <div class="flex rounded-lg border border-border overflow-hidden">
            {#each levelFilters as f (f.id)}
                <button
                    class="h-8 px-3 text-caption font-bold transition-colors
                        {filter === f.id
                            ? 'bg-blue text-white'
                            : 'text-text-secondary hover:text-text hover:bg-surface'}"
                    onclick={() => filter = f.id}
                >
                    {f.label}
                </button>
            {/each}
        </div>

        <div class="relative flex-1 basis-full md:basis-auto min-w-0 md:min-w-[200px]">
            <input
                type="text"
                placeholder="Search logs..."
                bind:value={search}
                class="w-full h-8 px-3 text-caption rounded-lg border border-border bg-input text-text placeholder-text-secondary focus:outline-none focus:border-blue {search ? 'pr-7' : ''}"
            />
            {#if search}
                <button
                    onclick={() => search = ''}
                    aria-label="Clear search"
                    class="absolute right-1.5 top-1/2 -translate-y-1/2 w-5 h-5 flex items-center justify-center rounded text-text-tertiary hover:text-text hover:bg-surface-hover transition-colors"
                >
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3.5 h-3.5">
                        <path d="M4.28 3.22a.75.75 0 0 0-1.06 1.06L6.94 8l-3.72 3.72a.75.75 0 1 0 1.06 1.06L8 9.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L9.06 8l3.72-3.72a.75.75 0 0 0-1.06-1.06L8 6.94 4.28 3.22Z"/>
                    </svg>
                </button>
            {/if}
        </div>

        <div class="flex items-center gap-2 ml-auto">
            {#if lastUpdated}
                <span class="text-caption text-text-tertiary hidden md:inline">
                    {lastUpdated.toLocaleTimeString()}
                </span>
            {/if}
            <button
                onclick={loadLogs}
                class="h-8 px-3 text-caption font-bold rounded-lg border border-border text-text-secondary hover:text-text hover:bg-surface transition-colors"
            >
                Refresh
            </button>
        </div>
    </div>

    <div
        bind:this={container}
        onscroll={handleScroll}
        class="bg-surface rounded-xl border border-border flex-1 min-h-0 overflow-y-auto"
    >
        {#if filtered.length === 0}
            <p class="p-4 text-body text-text-secondary text-center">
                {daemonLogs.length === 0 ? 'No log entries yet' : 'No matching entries'}
            </p>
        {:else}
            {#each filtered as entry, i (entry.timestamp + '-' + i)}
                <!-- Desktop: horizontal row -->
                <div class="hidden md:flex items-start gap-3 px-3 py-1.5 border-b border-border/50 last:border-b-0 font-mono text-caption">
                    <span class="text-text-secondary shrink-0 w-20">
                        {new Date(entry.timestamp).toLocaleTimeString()}
                    </span>
                    <span class="shrink-0 px-1.5 py-0.5 rounded text-micro uppercase font-bold
                        {entry.source === 'wgs2s' ? 'bg-purple/20 text-purple' : 'bg-aqua/20 text-aqua'}">
                        {entry.source === 'wgs2s' ? 'S2S' : 'TS'}
                    </span>
                    <span class="shrink-0 px-1.5 py-0.5 rounded text-micro uppercase font-bold {badgeColors[entry.level] ?? 'bg-border text-text-secondary'}">
                        {entry.level}
                    </span>
                    <span class="text-text break-all">{entry.message}</span>
                </div>
                <!-- Mobile: compact stacked -->
                <div class="md:hidden px-3 py-2 border-b border-border/50 last:border-b-0 font-mono text-caption">
                    <div class="flex items-center gap-2 mb-0.5">
                        <span class="text-text-secondary">
                            {new Date(entry.timestamp).toLocaleTimeString()}
                        </span>
                        <span class="px-1.5 py-0.5 rounded text-micro uppercase font-bold
                            {entry.source === 'wgs2s' ? 'bg-purple/20 text-purple' : 'bg-aqua/20 text-aqua'}">
                            {entry.source === 'wgs2s' ? 'S2S' : 'TS'}
                        </span>
                        <span class="px-1.5 py-0.5 rounded text-micro uppercase font-bold {badgeColors[entry.level] ?? 'bg-border text-text-secondary'}">
                            {entry.level}
                        </span>
                    </div>
                    <span class="text-text break-all leading-relaxed">{entry.message}</span>
                </div>
            {/each}
        {/if}
    </div>
</div>
