/** Synthetic `ParameterView` for specs; override any field. Bounds are integers (decimal bounds hit a known backend bug). */
export function parameterView(key: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key,
    valueType: 'number',
    effectiveValue: 3,
    defaultValue: 3,
    minValue: 1,
    maxValue: 10,
    allowedValues: null,
    description: 'Synthetic parameter',
    isSystemRequired: false,
    source: 'global',
    ...overrides
  };
}
