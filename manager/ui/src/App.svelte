<script>
    import { onMount } from 'svelte';
    import { connect, disconnect, getStatus, getErrors, getChangedFields, getUpdateInfo, dismissUpdate } from './lib/stores/tailscale.svelte.js';
    import { getDeviceInfo } from './lib/api.js';
    import TopBar from './lib/components/TopBar.svelte';
    import Sidebar from './lib/components/Sidebar.svelte';
    import DashboardTab from './lib/components/DashboardTab.svelte';
    import SettingsTab from './lib/components/SettingsTab.svelte';
    import LogsTab from './lib/components/LogsTab.svelte';
    import ErrorPanel from './lib/components/ErrorPanel.svelte';

    const VALID_TABS = new Set(['status', 'settings', 'logs']);
    const VALID_SUBTABS = new Set(['general', 'advanced', 'routing', 'integration', 'tunnels']);

    function parseHash() {
        const hash = location.hash.replace(/^#\/?/, '');
        if (!hash) return { tab: 'status', subTab: 'general' };
        const parts = hash.split('/');
        const tab = VALID_TABS.has(parts[0]) ? parts[0] : 'status';
        const subTab = (tab === 'settings' && VALID_SUBTABS.has(parts[1])) ? parts[1] : 'general';
        return { tab, subTab };
    }

    function navigate(tab, subTab) {
        const hash = tab === 'settings'
            ? '#/settings/' + (subTab || settingsSubTab)
            : tab === 'logs' ? '#/logs' : '#/';
        if (location.hash !== hash) location.hash = hash;
        activeTab = tab;
        if (tab === 'settings') settingsSubTab = subTab || settingsSubTab;
    }

    const initial = parseHash();
    let activeTab = $state(initial.tab);
    let settingsSubTab = $state(initial.subTab);
    let deviceInfo = $state(null);

    const status = getStatus();
    const errors = getErrors();
    const changedFields = getChangedFields();
    const updateInfo = getUpdateInfo();

    onMount(async () => {
        if (localStorage.getItem('theme') === 'light') {
            document.documentElement.classList.add('light');
        }

        function onHashChange() {
            const { tab, subTab } = parseHash();
            activeTab = tab;
            if (tab === 'settings') settingsSubTab = subTab;
        }
        window.addEventListener('hashchange', onHashChange);

        connect();
        deviceInfo = await getDeviceInfo();
        return () => {
            window.removeEventListener('hashchange', onHashChange);
            disconnect();
        };
    });

    function handleThemeToggle() {
        document.documentElement.classList.toggle('light');
        const isLight = document.documentElement.classList.contains('light');
        localStorage.setItem('theme', isLight ? 'light' : 'dark');
    }

    function handleNavigateIntegration() {
        navigate('settings', 'integration');
    }
</script>

<div class="h-screen flex flex-col bg-main pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))] md:pb-0">
    <TopBar
        hostname={deviceInfo?.hostname ?? ''}
        {status}
        {changedFields}
        onThemeToggle={handleThemeToggle}
        onNavigateIntegration={handleNavigateIntegration}
        {updateInfo}
        onDismissUpdate={dismissUpdate}
    />

    <div class="flex flex-1 min-h-0">
        <Sidebar {activeTab} onTabChange={(tab) => navigate(tab)} />

        <main class="flex-1 {['status', 'logs'].includes(activeTab) ? 'flex flex-col p-3 md:p-6 overflow-hidden' : 'overflow-y-auto p-3 md:p-6'}">
            {#if activeTab === 'status'}
                <DashboardTab {status} {deviceInfo} />
            {:else if activeTab === 'settings'}
                <SettingsTab {status} {deviceInfo} subTab={settingsSubTab} onSubTabChange={(sub) => navigate('settings', sub)} />
            {:else if activeTab === 'logs'}
                <LogsTab />
            {/if}
        </main>
    </div>

    <ErrorPanel {errors} />
</div>
