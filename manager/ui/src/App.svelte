<script>
    import { onMount } from 'svelte';
    import { connect, disconnect, getStatus, getErrors, getChangedFields, getUpdateInfo, dismissUpdate } from './lib/stores/tailscale.svelte.js';
    import { getDeviceInfo, initCsrf } from './lib/api.js';
    import TopBar from './lib/components/TopBar.svelte';
    import Sidebar from './lib/components/Sidebar.svelte';
    import DashboardTab from './lib/components/DashboardTab.svelte';
    import SettingsTab from './lib/components/SettingsTab.svelte';
    import LogsTab from './lib/components/LogsTab.svelte';
    import ErrorPanel from './lib/components/ErrorPanel.svelte';

    let activeTab = $state('status');
    let deviceInfo = $state(null);
    let settingsTarget = $state(null);

    const status = getStatus();
    const errors = getErrors();
    const changedFields = getChangedFields();
    const updateInfo = getUpdateInfo();

    onMount(async () => {
        if (localStorage.getItem('theme') === 'light') {
            document.documentElement.classList.add('light');
        }
        await initCsrf();
        connect();
        deviceInfo = await getDeviceInfo();
        return disconnect;
    });

    function handleThemeToggle() {
        document.documentElement.classList.toggle('light');
        const isLight = document.documentElement.classList.contains('light');
        localStorage.setItem('theme', isLight ? 'light' : 'dark');
    }

    function handleNavigateIntegration() {
        settingsTarget = 'integration';
        activeTab = 'settings';
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
        <Sidebar {activeTab} onTabChange={(tab) => activeTab = tab} />

        <main class="flex-1 {['status', 'logs'].includes(activeTab) ? 'flex flex-col p-3 md:p-6 overflow-hidden' : 'overflow-y-auto p-3 md:p-6'}">
            {#if activeTab === 'status'}
                <DashboardTab {status} {deviceInfo} />
            {:else if activeTab === 'settings'}
                <SettingsTab {status} {deviceInfo} {settingsTarget} onTargetConsumed={() => settingsTarget = null} />
            {:else if activeTab === 'logs'}
                <LogsTab />
            {/if}
        </main>
    </div>

    <ErrorPanel {errors} />
</div>
