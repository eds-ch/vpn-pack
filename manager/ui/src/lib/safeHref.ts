const SAFE_SCHEMES = ['https:', 'http:'];

export function safeHref(url: string | null | undefined): string {
    if (!url) return '#';
    try {
        const u = new URL(url);
        if (SAFE_SCHEMES.includes(u.protocol)) return u.toString();
    } catch {
        // not a valid URL - fall through
    }
    return '#';
}
