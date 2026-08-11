import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';

import TopBar from './TopBar.svelte';

function makeStatus(overrides = {}) {
    return {
        backendState: 'Running',
        tailscaleIPs: ['100.64.0.1'],
        connected: true,
        firewallHealth: null,
        health: [],
        integrationStatus: { configured: true, zbfEnabled: true },
        wgS2sTunnels: [],
        ...overrides,
    };
}

function makeUpdate(overrides = {}) {
    return {
        available: true,
        version: '2.0.0',
        currentVersion: '1.0.0',
        changelogURL: 'https://example.test/rel',
        dismissed: false,
        ...overrides,
    };
}

describe('TopBar update banner', () => {
    it('names the version and links the release notes', () => {
        render(TopBar, { status: makeStatus(), updateInfo: makeUpdate() });
        expect(screen.getByText(/Version 2\.0\.0 available/)).toBeInTheDocument();
        expect(screen.getByRole('link', { name: 'Release notes' })).toBeInTheDocument();
    });

    // get.sh is a bash script (`set -euo pipefail` on line 12). The banner used
    // to hand out `| sh`, which on UniFi OS is dash: it aborts with
    // "Illegal option -o pipefail" and installs nothing.
    it('gives an install command that runs get.sh with bash, never sh', () => {
        render(TopBar, { status: makeStatus(), updateInfo: makeUpdate() });
        const cmd = screen.getByText(/^curl -fsSL/).textContent;
        expect(cmd).toMatch(/\|\s*bash$/);
        expect(cmd).not.toMatch(/\|\s*sh$/);
    });

    it('stays hidden when no update is available', () => {
        render(TopBar, { status: makeStatus(), updateInfo: makeUpdate({ available: false }) });
        expect(screen.queryByText(/available/)).not.toBeInTheDocument();
    });

    it('stays hidden once dismissed', () => {
        render(TopBar, { status: makeStatus(), updateInfo: makeUpdate({ dismissed: true }) });
        expect(screen.queryByText(/Version 2\.0\.0 available/)).not.toBeInTheDocument();
    });
});
