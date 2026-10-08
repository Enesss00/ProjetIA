import { test, expect } from '@playwright/test';

// End-to-end: boot the menu, start a seeded game, watch an attack unfold,
// issue defensive commands, and confirm the game reaches an end state with a
// report. This drives the real Go server + built client.

test('full game: menu → defend → end report', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(String(e)));

  await page.goto('/');
  await expect(page.locator('.logo')).toContainText('GHOST');

  await page.fill('#seed-input', '1337');
  await page.selectOption('#profile-select', 'smash');
  await page.click('#btn-start');

  // Game HUD appears and the clock advances.
  await expect(page.locator('#hud-top')).toBeVisible();
  await expect(page.locator('#hud-clock')).not.toHaveText('--:--');

  // The terminal accepts commands.
  const term = page.locator('#terminal');
  await term.click();
  await page.keyboard.type('status');
  await page.keyboard.press('Enter');
  await page.keyboard.type('scan net');
  await page.keyboard.press('Enter');

  // Speed up to reach an outcome quickly.
  await page.click('#spd-4');

  // The game ends (smash profile breaches fast) and shows a report.
  await expect(page.locator('#end')).toBeVisible({ timeout: 40000 });
  await expect(page.locator('#end-report')).toContainText('Incident Report');
  expect(errors, 'no uncaught page errors').toEqual([]);
});

test('garbage terminal input never breaks the client', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto('/?seed=42&profile=stealth');
  await expect(page.locator('#hud-top')).toBeVisible();
  const term = page.locator('#terminal');
  await term.click();
  // Control chars, huge token, unknown command.
  await page.keyboard.insertText('scan ' + 'A'.repeat(300));
  await page.keyboard.press('Enter');
  await page.keyboard.type('!!!@#$%^&*()');
  await page.keyboard.press('Enter');
  await page.keyboard.type('isolate nonexistent-host');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(500);
  await expect(page.locator('#hud-clock')).toBeVisible();
  expect(errors).toEqual([]);
});
