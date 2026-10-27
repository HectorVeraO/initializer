import { MenuItem } from '@openng/optimus-ui/api';

/** Production replacement of `dev.menu.ts`: there is no developer section. */
export function createDeveloperSection(label: string): MenuItem | null {
  void label;

  return null;
}
