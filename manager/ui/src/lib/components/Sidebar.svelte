<script>
    import Icon from './Icon.svelte';

    let { activeTab = 'status', onTabChange } = $props();

    const mainTabs = [
        { id: 'status', icon: 'layout-dashboard', label: 'Dashboard' },
    ];

    const utilTabs = [
        { id: 'settings', icon: 'settings', label: 'Settings' },
        { id: 'logs', icon: 'scroll-text', label: 'Logs' },
    ];

    const allTabs = [...mainTabs, ...utilTabs];
</script>

<!-- Desktop sidebar -->
<nav class="hidden md:flex bg-panel border-r border-border w-[50px] flex-col items-center pt-4 gap-3 shrink-0">
    {#each mainTabs as tab (tab.id)}
        <div class="relative group">
            <button
                class="w-8 h-8 rounded-full flex items-center justify-center transition-colors
                    {activeTab === tab.id
                        ? 'bg-blue text-white'
                        : 'text-text-secondary hover:text-text'}"
                onclick={() => onTabChange(tab.id)}
                aria-label={tab.label}
            >
                <Icon name={tab.icon} size={18} />
            </button>
            <span class="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2 py-1 text-caption font-bold text-white bg-[#1c1e21] rounded whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 transition-opacity z-50">
                {tab.label}
            </span>
        </div>
    {/each}

    <div class="w-5 border-t border-border"></div>

    {#each utilTabs as tab (tab.id)}
        <div class="relative group">
            <button
                class="w-8 h-8 rounded-full flex items-center justify-center transition-colors
                    {activeTab === tab.id
                        ? 'bg-blue text-white'
                        : 'text-text-secondary hover:text-text'}"
                onclick={() => onTabChange(tab.id)}
                aria-label={tab.label}
            >
                <Icon name={tab.icon} size={18} />
            </button>
            <span class="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2 py-1 text-caption font-bold text-white bg-[#1c1e21] rounded whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 transition-opacity z-50">
                {tab.label}
            </span>
        </div>
    {/each}

    <div class="mt-auto pb-4 relative group">
        <a
            href="/network/"
            class="w-8 h-8 rounded-full flex items-center justify-center text-text-secondary hover:text-text transition-colors"
            aria-label="UniFi Network"
        >
            <Icon name="ubiquiti" size={18} />
        </a>
        <span class="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2 py-1 text-caption font-bold text-white bg-[#1c1e21] rounded whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 transition-opacity z-50">
            UniFi Network
        </span>
    </div>
</nav>

<!-- Mobile bottom navigation -->
<nav class="md:hidden fixed bottom-0 left-0 right-0 z-40 bg-panel border-t border-border" style="padding-bottom: env(safe-area-inset-bottom, 0px);">
    <div class="flex items-center justify-around h-14">
        {#each allTabs as tab (tab.id)}
            <button
                class="flex flex-col items-center justify-center gap-0.5 flex-1 h-full transition-colors
                    {activeTab === tab.id
                        ? 'text-blue'
                        : 'text-text-secondary active:text-text'}"
                onclick={() => onTabChange(tab.id)}
                aria-label={tab.label}
            >
                <Icon name={tab.icon} size={20} />
                <span class="text-micro">{tab.label}</span>
            </button>
        {/each}
        <a
            href="/network/"
            class="flex flex-col items-center justify-center gap-0.5 flex-1 h-full text-text-secondary active:text-text transition-colors"
            aria-label="UniFi Network"
        >
            <Icon name="ubiquiti" size={20} />
            <span class="text-micro">UniFi</span>
        </a>
    </div>
</nav>
