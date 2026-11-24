import { defineConfig } from '@playwright/test';

if (process.env.LIGHTHOUSE_INTEGRATION !== '1') {
  throw new Error('Start tests/integration/compose.yaml and set LIGHTHOUSE_INTEGRATION=1 before running browser integration tests.');
}

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 60000,
  expect: { timeout: 30000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  failOnFlakyTests: true,
  reporter: [['list'], ['json', { outputFile: './test-results/results.json' }], ['html', { open: 'never' }]],
  use: { browserName: 'chromium', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'hub-desktop', metadata: { mode: 'hub' }, use: { baseURL: 'http://127.0.0.1:18080', viewport: { width: 1440, height: 900 } } },
    { name: 'hub-mobile', metadata: { mode: 'hub' }, use: { baseURL: 'http://127.0.0.1:18080', viewport: { width: 390, height: 844 } } },
    { name: 'standalone-desktop', metadata: { mode: 'standalone' }, use: { baseURL: 'http://127.0.0.1:18081', viewport: { width: 1440, height: 900 } } },
    { name: 'standalone-mobile', metadata: { mode: 'standalone' }, use: { baseURL: 'http://127.0.0.1:18081', viewport: { width: 390, height: 844 } } },
  ],
});