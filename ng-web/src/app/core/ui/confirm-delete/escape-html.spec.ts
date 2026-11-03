import { escapeHtml } from './escape-html';

describe('escapeHtml', () => {
  it('turns the characters that end an HTML fragment into their entities', () => {
    expect(escapeHtml('<b>Pan & "mermelada"</b>')).toBe('&lt;b&gt;Pan &amp; &quot;mermelada&quot;&lt;/b&gt;');
  });

  it('escapes single quotes so a value cannot close an attribute', () => {
    expect(escapeHtml("it's")).toBe('it&#39;s');
  });

  it('leaves text without special characters as it is', () => {
    expect(escapeHtml('tenant-a')).toBe('tenant-a');
  });
});
