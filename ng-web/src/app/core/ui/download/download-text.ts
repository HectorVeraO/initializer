/**
 * Offers `text` to the person as a file download, byte for byte: the Blob is
 * built from the exact string, never from a re-serialization.
 */
export function downloadText(filename: string, text: string): void {
  const blob = new Blob([text], { type: 'application/json' });

  const url = URL.createObjectURL(blob);

  const anchor = document.createElement('a');

  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(url);
}
