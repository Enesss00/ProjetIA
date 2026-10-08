import { defineConfig, devices } from '@playwright/test';

// The Go server is built and launched by Playwright itself, serving the built
// client from ../web/dist. This is identical to how CI runs the e2e suite.
const PORT = Number(process.env.GN_PORT ?? 8191);

export default defineConfig({
  testDir: './e2e',
  timeout: 60000,
  fullyParallel: false,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: `http://localhost:${PORT}`,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `../server/ghostnet serve -addr :${PORT} -static ./dist`,
    url: `http://localhost:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 30000,
  },
});
