import { describe, it, expect } from 'vitest';
import { safeHref } from './safeHref.ts';

// SEC-C20 / SEC-C21: control-server-provided URLs (authURL, changelogURL) are
// rendered directly into <a href={...}>. Without scheme validation, a hostile
// control-plane response can deliver `javascript:alert(1)` or `data:text/html,...`
// straight into the DOM.
describe('safeHref', () => {
    it('returns "#" for javascript: scheme', () => {
        expect(safeHref('javascript:alert(1)')).toBe('#');
    });

    it('returns "#" for data: scheme', () => {
        expect(safeHref('data:text/html,<script>alert(1)</script>')).toBe('#');
    });

    it('returns "#" for vbscript: scheme', () => {
        expect(safeHref('vbscript:msgbox')).toBe('#');
    });

    it('returns "#" for null/undefined/empty', () => {
        expect(safeHref(null)).toBe('#');
        expect(safeHref(undefined)).toBe('#');
        expect(safeHref('')).toBe('#');
    });

    it('returns "#" for malformed URL', () => {
        expect(safeHref('not a url')).toBe('#');
    });

    it('passes https URLs through', () => {
        expect(safeHref('https://login.tailscale.com/a/abc')).toBe('https://login.tailscale.com/a/abc');
    });

    it('passes http URLs through', () => {
        expect(safeHref('http://example.com/')).toBe('http://example.com/');
    });
});
