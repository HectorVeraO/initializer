import {
  test,
  Browser,
  BrowserContext,
  BrowserContextOptions,
  Page,
} from '@playwright/test';
import { manualContextOptions } from '../env';

/**
 * Opens a secondary browser context with the suite's viewport and, when
 * PW_VIDEO=on, a recording aimed at the current test's folder. Use it instead of
 * `browser.newContext()` and close it with `closeAndAttachVideo()`.
 *
 * @param browser The fixture's browser.
 * @param options Extra context options; a `viewport` here overrides the default.
 */
export async function newRecordedContext(
  browser: Browser,
  options: BrowserContextOptions = {},
): Promise<BrowserContext> {
  const { viewport, ...recording } = manualContextOptions(
    test.info().outputPath(),
  );
  const context = await browser.newContext({
    viewport,
    ...options,
    ...recording,
  });
  return context;
}

/**
 * Saves and attaches the video of one page of a manual context. Playwright
 * instruments manual contexts for traces but not for video: the .webm stays loose
 * as `page@<hash>.webm` and `playwright show-report` lists only attachments.
 *
 * Call it AFTER `context.close()` (the file is complete only then), with the
 * handle taken from `page.video()` BEFORE closing.
 *
 * @param video The page's video, or null when recording is off.
 * @param name Attachment and file name, without extension.
 */
export async function attachContextVideo(
  video: ReturnType<Page['video']>,
  name: string,
): Promise<void> {
  if (video === null) {
    return;
  }
  const info = test.info();
  const filePath = info.outputPath(`${name}.webm`);
  await video.saveAs(filePath);
  // saveAs copies; deleting the original avoids a duplicate page@<hash>.webm.
  await video.delete();
  await info.attach(name, { path: filePath, contentType: 'video/webm' });
}

/**
 * Closes a context opened with `newRecordedContext()` and attaches the video of
 * each of its pages. Replaces a bare `context.close()`.
 *
 * @param context The context to close.
 * @param name Base attachment name; numbered when the context had several pages.
 */
export async function closeAndAttachVideo(
  context: BrowserContext,
  name: string,
): Promise<void> {
  // Video handles must be taken before closing; the files complete after it.
  const videos = context.pages().map((page) => page.video());
  await context.close();
  for (const [index, video] of videos.entries()) {
    const label = videos.length === 1 ? name : `${name}-${index + 1}`;
    await attachContextVideo(video, label);
  }
}
