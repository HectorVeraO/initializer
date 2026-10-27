export const STORAGE_KEYS = {
  USER_THEME: 'app_user_theme',
  USER_SETTINGS: 'app_user_settings'
} as const;

export type StorageKey = (typeof STORAGE_KEYS)[keyof typeof STORAGE_KEYS];
