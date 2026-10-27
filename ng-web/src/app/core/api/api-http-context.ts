import { HttpContextToken } from '@angular/common/http';

/** Set by callers that present their own error state, so no generic toast is raised. */
export const SKIP_ERROR_TOAST = new HttpContextToken<boolean>(() => false);
