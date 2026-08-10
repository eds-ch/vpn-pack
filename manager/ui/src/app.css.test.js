import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const css = readFileSync(resolve(process.cwd(), 'src/app.css'), 'utf8');

const LENGTH = /^(0|[\d.]+(rem|em|px|%)?)$/;

describe('typography tokens', () => {
    // Regression guard. --text-* is Tailwind's font-size namespace. Declaring a
    // colour there (we had --text-heading: #f9fafa) makes font-size resolve to a
    // colour, so the declaration is dropped and the size silently inherits.
    it('keeps --text-* holding lengths only, never colours', () => {
        const offenders = [...css.matchAll(/(--text-[a-z-]*?)\s*:\s*([^;]+);/g)]
            .map(([, name, value]) => [name, value.trim()])
            .filter(([name, value]) => !name.endsWith('--font-weight') && !LENGTH.test(value));

        expect(offenders).toEqual([]);
    });

    it('exposes exactly the four size tiers', () => {
        const sizes = [...css.matchAll(/(--text-[a-z]+)\s*:\s*[\d.]+rem;/g)].map(m => m[1]);
        expect(sizes.sort()).toEqual(['--text-body', '--text-caption', '--text-page', '--text-section']);
    });

    // The badge sits inside monospace log rows; without an explicit family it
    // inherits the mono face and becomes the only badge set in a different font.
    it('pins a font-family on the badge utility', () => {
        const badge = css.match(/@utility badge-label \{([^}]+)\}/)?.[1] ?? '';
        expect(badge).toMatch(/font-family:\s*var\(--font-sans\)/);
    });
});
