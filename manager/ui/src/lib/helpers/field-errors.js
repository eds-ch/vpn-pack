const INPUT_BASE = 'mt-1 w-full px-3 py-1.5 text-body rounded-lg bg-input text-text placeholder-text-secondary focus:outline-none transition-colors';
const INPUT_ERROR = `${INPUT_BASE} border-2 border-error focus:border-error`;
const INPUT_NORMAL = `${INPUT_BASE} border border-border focus:border-blue`;

export function inputClass(fieldErrors, field) {
    return fieldErrors[field] ? INPUT_ERROR : INPUT_NORMAL;
}

export function clearFieldError(fieldErrors, field) {
    if (!fieldErrors[field]) return fieldErrors;
    const next = { ...fieldErrors };
    delete next[field];
    return next;
}
