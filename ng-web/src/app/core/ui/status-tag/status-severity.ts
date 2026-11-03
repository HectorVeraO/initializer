import { BadgeSeverity } from '@openng/optimus-ui/types/badge';

/** Every closed set of values the console shows as a tag. One family, one look on every page. */
export type StatusFamily = 'active' | 'tenantStatus' | 'membershipStatus' | 'invitationStatus' | 'apiKeyStatus';

export interface StatusEntry {
  severity: BadgeSeverity;
  labelKey: string;
}

/**
 * The single value -> severity -> label table. Urgent states carry color
 * (warn, danger, success); categories are neutral (secondary) so a status
 * column next to a type column stays readable. The labels are the shared state
 * words of the console (`statuses.*`), so a tag reads the same on every screen.
 */
export const STATUS_TABLE: Readonly<Record<StatusFamily, Readonly<Record<string, StatusEntry>>>> = {
  active: {
    true: { severity: 'success', labelKey: 'statuses.active' },
    false: { severity: 'secondary', labelKey: 'statuses.inactive' }
  },
  tenantStatus: {
    active: { severity: 'success', labelKey: 'statuses.active' },
    suspended: { severity: 'warn', labelKey: 'statuses.suspended' }
  },
  membershipStatus: {
    active: { severity: 'success', labelKey: 'statuses.active' },
    suspended: { severity: 'warn', labelKey: 'statuses.suspended' },
    revoked: { severity: 'danger', labelKey: 'statuses.revoked' }
  },
  invitationStatus: {
    pending: { severity: 'secondary', labelKey: 'statuses.pending' },
    accepted: { severity: 'success', labelKey: 'statuses.accepted' },
    revoked: { severity: 'danger', labelKey: 'statuses.revoked' },
    expired: { severity: 'warn', labelKey: 'statuses.expired' }
  },
  apiKeyStatus: {
    active: { severity: 'success', labelKey: 'statuses.active' },
    expired: { severity: 'warn', labelKey: 'statuses.expired' },
    revoked: { severity: 'danger', labelKey: 'statuses.revoked' }
  }
};

/** What a tag shows for one value: the severity, the translation key (null for a value the table does not know) and the raw text. */
export interface StatusView {
  severity: BadgeSeverity;
  labelKey: string | null;
  raw: string;
}

/**
 * Resolves a value of a family. A value the table does not know (a new server
 * enum) keeps its raw text on a neutral tag; null and empty text resolve to null.
 */
export function resolveStatus(family: StatusFamily, value: string | boolean | null | undefined): StatusView | null {
  if (value === null || value === undefined || value === '') {
    return null;
  }

  const raw = String(value);

  const entry = STATUS_TABLE[family][raw];

  if (entry === undefined) {
    return { severity: 'secondary', labelKey: null, raw };
  }

  return { severity: entry.severity, labelKey: entry.labelKey, raw };
}
