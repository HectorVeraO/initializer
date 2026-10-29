import { BRAND } from '../app/core/config/brand';

export const environment = {
  production: false,
  appName: `${BRAND} - Development`,
  trace: {
    samplingRate: 1.0
  }
};
