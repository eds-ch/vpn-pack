import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';

import Icon from './Icon.svelte';

describe('Icon', () => {
    it('renders an svg for a valid icon name', () => {
        const { container } = render(Icon, { name: 'check' });
        const svg = container.querySelector('svg');
        expect(svg).not.toBeNull();
        expect(svg.querySelector('path')).not.toBeNull();
    });

    it('renders nothing for an unknown icon name', () => {
        const { container } = render(Icon, { name: 'does-not-exist' });
        expect(container.querySelector('svg')).toBeNull();
    });

    it('does not resolve inherited prototype properties as icons', () => {
        for (const name of ['toString', 'constructor', 'hasOwnProperty', '__proto__']) {
            const { container } = render(Icon, { name });
            expect(container.querySelector('svg')).toBeNull();
        }
    });
});
