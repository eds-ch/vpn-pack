const SEVERITY_RANK = { high: 3, medium: 2, low: 1 };

export function pickPrimaryWarning(warnings) {
    if (!Array.isArray(warnings) || warnings.length === 0) return null;
    return [...warnings].sort((a, b) => {
        const bySeverity = (SEVERITY_RANK[b.severity] ?? 0) - (SEVERITY_RANK[a.severity] ?? 0);
        if (bySeverity !== 0) return bySeverity;
        const ai = !!a.impactsConnectivity, bi = !!b.impactsConnectivity;
        if (ai !== bi) return ai ? -1 : 1;
        return (a.code ?? '').localeCompare(b.code ?? '');
    })[0];
}

export function waitingReason(status) {
    if (status?.connected === false) {
        return { title: 'Waiting for tailscaled...', text: '' };
    }
    const warning = pickPrimaryWarning(status?.health);
    if (warning) {
        return { title: warning.title || warning.code || '', text: warning.text || '' };
    }
    return { title: 'Connecting to the Tailscale coordination server...', text: '' };
}
