export type ParameterValueType = 'string' | 'number' | 'boolean' | 'json';

export type ParameterSource = 'global' | 'tenant';

/** One parameter as listed: the definition plus the value in effect for the active tenant. */
export interface ParameterView {
  key: string;
  valueType: ParameterValueType;
  effectiveValue: unknown;
  defaultValue: unknown;
  minValue: number | null;
  maxValue: number | null;
  allowedValues: unknown[] | null;
  description: string;
  isSystemRequired: boolean;
  source: ParameterSource;
}

/** Body of POST /api/v1/parameters. */
export interface ParameterCreate {
  key: string;
  valueType: ParameterValueType;
  defaultValue: unknown;
  minValue?: number | null;
  maxValue?: number | null;
  allowedValues?: unknown[] | null;
  description?: string;
  isSystemRequired?: boolean;
}

/** Body of PUT /api/v1/parameters/{key}: the mutable fields of a definition. */
export type ParameterReplace = Omit<ParameterCreate, 'key'>;

/** `data` of DELETE /api/v1/parameters/{key}. */
export interface ParameterDeleted {
  key: string;
  deletedOverrideCount: number;
}
