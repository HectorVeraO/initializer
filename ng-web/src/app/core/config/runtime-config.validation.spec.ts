import { validateRuntimeConfig } from './runtime-config.validation';

describe('validateRuntimeConfig', () => {
  it('accepts an empty apiUrl', () => {
    expect(validateRuntimeConfig({ apiUrl: '' })).toEqual({ ok: true, config: { apiUrl: '' } });
  });

  it('rejects an absolute URL', () => {
    expect(validateRuntimeConfig({ apiUrl: 'http://localhost:10100' })).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a path', () => {
    expect(validateRuntimeConfig({ apiUrl: '/api' })).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a protocol-relative URL', () => {
    expect(validateRuntimeConfig({ apiUrl: '//evil.test' })).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a blank string', () => {
    expect(validateRuntimeConfig({ apiUrl: ' ' })).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a missing apiUrl', () => {
    expect(validateRuntimeConfig({})).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a null apiUrl', () => {
    expect(validateRuntimeConfig({ apiUrl: null })).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a string document', () => {
    expect(validateRuntimeConfig('')).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });

  it('rejects a null document', () => {
    expect(validateRuntimeConfig(null)).toEqual({ ok: false, missingFields: ['apiUrl'] });
  });
});
