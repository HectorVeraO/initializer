/**
 * Release flag that picks the restyled console. The shell and each screen read
 * it at their own boundary through `FLAG_SOURCE`; a shared widget never does.
 */
export const UI_RESTYLE_FLAG = 'ui_restyle';
