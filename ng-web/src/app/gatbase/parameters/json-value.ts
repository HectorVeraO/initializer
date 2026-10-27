/** Result of reading the text of a JSON editor. */
export type JsonTextResult = { readonly ok: true; readonly value: unknown } | { readonly ok: false };

/** The text a JSON editor shows for `value`; `parseJsonText` reads it back to an equal value. */
export function jsonText(value: unknown): string {
  const text = JSON.stringify(value) ?? 'null';

  return text;
}

/** Parses editor text. Empty or malformed text is not ok, so nothing is sent. */
export function parseJsonText(text: string): JsonTextResult {
  try {
    const value: unknown = JSON.parse(text);

    return { ok: true, value };
  } catch {
    return { ok: false };
  }
}

/** Compact display of a value in a table cell: strings as they are, everything else as JSON. */
export function displayValue(value: unknown): string {
  const text = typeof value === 'string' ? value : jsonText(value);

  return text;
}
