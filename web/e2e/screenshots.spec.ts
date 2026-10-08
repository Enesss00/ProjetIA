import { test } from '@playwright/test';

// This spec doubles as the screenshot generator for /docs/screenshots.
// Run with:  npx playwright test screenshots  (server managed by Playwright)
const OUT = '../docs/screenshots';

test('capture docs screenshots', async ({ page }) => {
  test.setTimeout(150000);
  await page.setViewportSize({ width: 1440, height: 900 });

  // 1. Menu
  await page.goto('/');
  await page.waitForTimeout(900);
  await page.screenshot({ path: `${OUT}/01-menu.png` });

  // Start a game and drive the terminal.
  await page.fill('#seed-input', '1337');
  await page.selectOption('#profile-select', 'apt');
  await page.click('#btn-start');
  await page.waitForSelector('#hud-top');
  await page.locator('#terminal').click();
  for (const c of ['help', 'status', 'scan web-01', 'forensics vpn-01']) {
    await page.keyboard.type(c);
    await page.keyboard.press('Enter');
    await page.waitForTimeout(600);
  }
  await page.click('#spd-4');
  await page.waitForTimeout(9000);

  // 2. Attack in progress (terminal now populated).
  await page.screenshot({ path: `${OUT}/02-attack.png` });

  // 3. End report.
  await page.waitForSelector('#end', { timeout: 60000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${OUT}/04-report.png` });

  // 4. Replay view.
  await page.click('#btn-replay');
  await page.waitForTimeout(400);
  const slider = page.locator('#replay-slider');
  await slider.fill('250');
  await page.waitForTimeout(1200);
  await page.screenshot({ path: `${OUT}/03-replay.png` });
});
