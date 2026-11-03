import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

/**
 * Clicks one row action. The restyled tables keep the two first actions as
 * inline buttons and fold three or more into the row menu; the previous tables
 * render their own buttons or links. The action carries the same test id in
 * both, so callers never branch on the template.
 */
export async function clickRowAction(
  row: Locator,
  testId: string,
): Promise<void> {
  const inline = row.getByTestId(testId);
  if ((await inline.count()) > 0) {
    await inline.click();
    return;
  }

  const page = row.page();
  await row.getByTestId('row-actions-trigger').click();
  await page.getByTestId(testId).click();
}

/**
 * Runs a destructive row action and accepts it. The restyled screens ask once
 * in the shared confirmation dialog, whose buttons carry no test id; the
 * previous screens ask with the inline two-step button `<testId>-confirm`.
 * Both are one click and neither asks for a typed keyword.
 */
export async function confirmRowAction(
  page: Page,
  row: Locator,
  testId: string,
  acceptLabel: string,
): Promise<void> {
  await clickRowAction(row, testId);

  const inlineConfirm = row.getByTestId(`${testId}-confirm`);
  const dialogAccept = page
    .locator('[role="alertdialog"]')
    .getByRole('button', { name: acceptLabel, exact: true });

  await expect(inlineConfirm.or(dialogAccept)).toBeVisible();

  if (await inlineConfirm.isVisible()) {
    await inlineConfirm.click();
    return;
  }

  await dialogAccept.click();
}
