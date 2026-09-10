import * as crypto from 'node:crypto';
import { request } from '@playwright/test';
import {
  deleteThrowawayUser,
  expect,
  LOGGED_OUT,
  makeThrowawayUser,
  test,
  waitForLoading,
} from './fixtures';
import { API_BASE_URL } from './global-setup';

// N8 (issue #158): the full two-factor auth lifecycle through the real UI —
// enroll on /settings (QR/manual key + confirm + one-time recovery codes),
// then prove interactive login now requires the second factor (TOTP and, in
// a second pass, a single-use recovery code).
test.use({ storageState: LOGGED_OUT });

// ---------------------------------------------------------------------------
// RFC 6238 TOTP (6 digits, HMAC-SHA1, 30s period) + base32 decode, using only
// Node's crypto. A Playwright spec runs in Node, so this is the authenticator
// app the enrollment dialog would otherwise be scanned by.
// ---------------------------------------------------------------------------
const BASE32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function base32Decode(input: string): Buffer {
  const clean = input.replace(/=+$/, '').toUpperCase();
  let bits = 0;
  let value = 0;
  const out: number[] = [];
  for (const ch of clean) {
    const idx = BASE32.indexOf(ch);
    if (idx === -1) throw new Error(`invalid base32 char: ${ch}`);
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((value >> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(out);
}

function totp(secret: string, when = new Date()): string {
  const key = base32Decode(secret);
  const counter = Math.floor(when.getTime() / 1000 / 30);
  const buf = Buffer.alloc(8);
  buf.writeBigUInt64BE(BigInt(counter), 0);
  const hmac = crypto.createHmac('sha1', key).update(buf).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const binary =
    ((hmac[offset] & 0x7f) << 24) |
    ((hmac[offset + 1] & 0xff) << 16) |
    ((hmac[offset + 2] & 0xff) << 8) |
    (hmac[offset + 3] & 0xff);
  return String(binary % 1_000_000).padStart(6, '0');
}

const RECOVERY_CODE_RE = /[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}/;

test.describe('Two-factor authentication', () => {
  test('enrolls, then signs in with a TOTP code and a recovery code', async ({ page }) => {
    // A dedicated account so enabling 2FA can never disturb the shared
    // TEST_USER every other spec authenticates as.
    const user = makeThrowawayUser('twofa');

    // Register via API, then log in through the UI.
    const reg = await page.request.post(`${API_BASE_URL}/register`, { data: user });
    expect(reg.ok(), `registration should succeed: ${await reg.text()}`).toBeTruthy();

    try {
      await page.goto('/');
      await page.getByLabel(/username or email/i).fill(user.username);
      await page.getByLabel(/password/i).fill(user.password);
      await page.getByRole('button', { name: /login/i }).click();
      await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible({
        timeout: 15000,
      });

      // --- Enrollment ---
      await page.goto('/settings');
      await waitForLoading(page);

      await page.getByText('Enable two-factor authentication').click();
      await expect(page.getByLabel('Manual setup key')).toBeVisible();

      const secret = await page.getByLabel('Manual setup key').inputValue();
      expect(secret).toMatch(/^[A-Z2-7]+=*$/); // base32

      // A wrong code is rejected inline (the error renders inside the dialog).
      await page.getByLabel('Verification code *').fill('000000');
      await page.getByRole('button', { name: /enable and continue/i }).click();
      await expect(page.locator('.MuiDialog-root').getByText(/invalid code/i)).toBeVisible();

      // The real code confirms enrollment and mints recovery codes.
      await page.getByLabel('Verification code *').fill(totp(secret));
      await page.getByRole('button', { name: /enable and continue/i }).click();

      await expect(page.getByText(/two-factor authentication enabled/i)).toBeVisible({
        timeout: 10000,
      });
      const recoveryDialog = page.locator('.MuiDialog-root').filter({ hasText: 'Recovery codes' });
      await expect(recoveryDialog).toBeVisible();
      const recoveryCodes = await recoveryDialog.getByText(RECOVERY_CODE_RE).allTextContents();
      expect(recoveryCodes).toHaveLength(10);
      for (const code of recoveryCodes) {
        expect(code).toMatch(RECOVERY_CODE_RE);
      }
      await recoveryDialog.getByRole('button', { name: /done/i }).click();

      // --- Sign-out then sign-in now requires the second factor ---
      await page.getByRole('button', { name: /logout/i }).click();
      await expect(page.getByRole('heading', { name: /login/i })).toBeVisible({ timeout: 10000 });

      await page.getByLabel(/username or email/i).fill(user.username);
      await page.getByLabel(/password/i).fill(user.password);
      await page.getByRole('button', { name: /login/i }).click();

      // Password alone must NOT reach the dashboard: the 2FA step appears.
      await expect(page.getByRole('heading', { name: 'Two-factor authentication' })).toBeVisible();
      await expect(page.getByRole('heading', { name: /dashboard/i })).not.toBeVisible();

      // A wrong code shows an error and stays on the 2FA step.
      await page.getByLabel('Verification code *').fill('000000');
      await page.getByRole('button', { name: /login/i }).click();
      await expect(page.getByText(/invalid code/i)).toBeVisible();
      await expect(page.getByRole('heading', { name: /dashboard/i })).not.toBeVisible();

      // The correct TOTP code completes the login. Accepted TOTP steps are
      // single-use (issue #873): enrollment above already spent the current
      // step, so this login must present the *next* step's code — still inside
      // the server's ±1 skew window, but strictly greater than the burned step.
      await page
        .getByLabel('Verification code *')
        .fill(totp(secret, new Date(Date.now() + 30_000)));
      await page.getByRole('button', { name: /login/i }).click();
      await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible({
        timeout: 10000,
      });

      // --- Recovery code path: lose the authenticator, still get in ---
      await page.getByRole('button', { name: /logout/i }).click();
      await expect(page.getByRole('heading', { name: /login/i })).toBeVisible({ timeout: 10000 });

      await page.getByLabel(/username or email/i).fill(user.username);
      await page.getByLabel(/password/i).fill(user.password);
      await page.getByRole('button', { name: /login/i }).click();
      await expect(page.getByRole('heading', { name: 'Two-factor authentication' })).toBeVisible();

      await page.getByLabel('Verification code *').fill(recoveryCodes[0]);
      await page.getByRole('button', { name: /login/i }).click();
      await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible({
        timeout: 10000,
      });
    } finally {
      // Admin cleanup (the shared TEST_USER is auto-admin): hard-delete the
      // throwaway account so nothing accumulates across runs.
      const admin = await request.newContext({ storageState: 'playwright/.auth/user.json' });
      try {
        await deleteThrowawayUser(admin, user.username);
      } finally {
        await admin.dispose();
      }
    }
  });
});
