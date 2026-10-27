/** Deployment-time settings served as `assets/config/runtime-config.json`. */
export interface RuntimeConfig {
  apiUrl: string;
}

/** Why `runtime-config.json` could not be used. */
export type RuntimeConfigFailure = { kind: 'load' } | { kind: 'invalid'; missingFields: string[] };
