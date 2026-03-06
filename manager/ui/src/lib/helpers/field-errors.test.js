import { describe, it, expect } from 'vitest';
import { inputClass, clearFieldError } from './field-errors.js';

describe('inputClass', () => {
    it('returns error class when field has error', () => {
        const result = inputClass({ name: 'required' }, 'name');
        expect(result).toContain('border-error');
    });

    it('returns normal class when field has no error', () => {
        const result = inputClass({}, 'name');
        expect(result).toContain('border-border');
        expect(result).not.toContain('border-error');
    });
});

describe('clearFieldError', () => {
    it('returns new object without the field error', () => {
        const errors = { name: 'required', port: 'invalid' };
        const result = clearFieldError(errors, 'name');
        expect(result).toEqual({ port: 'invalid' });
        expect(result).not.toBe(errors);
    });

    it('returns same reference when field has no error', () => {
        const errors = { port: 'invalid' };
        const result = clearFieldError(errors, 'name');
        expect(result).toBe(errors);
    });

    it('returns same reference for empty errors', () => {
        const errors = {};
        const result = clearFieldError(errors, 'name');
        expect(result).toBe(errors);
    });
});
