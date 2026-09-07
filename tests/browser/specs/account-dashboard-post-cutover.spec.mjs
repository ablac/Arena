import { expect, test } from '@playwright/test';

// This is the post-cutover identity shape, intentionally without an email.
// Account eligibility must be decided by the shared policy rather than the
// Dashboard's former email-field gate.
const ACCOUNT = {
  id: 'acct-post-cutover',
  email: '',
  email_verified: true,
  display_name: 'Private Real Name',
  public_username: 'arena_pilot',
};

async function installDashboardRoutes(page, requested, username = ACCOUNT.public_username) {
  const account = {...ACCOUNT, public_username: username};
  await page.route('https://fonts.googleapis.com/**', (route) => route.fulfill({
    body: '', contentType: 'text/css; charset=utf-8',
  }));
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/arena(?=\/)/, '');
    requested.add(`${url.pathname}${url.search}`);
    const payloads = {
      '/api/v1/account/session': { authenticated: true, csrf_token: 'fixture-csrf', account },
      '/api/v1/account/cosmetics': { account, bots: [], licenses: [], subscription: null },
      '/api/v1/cosmetics/catalog': {
        categories: [], items: [], packs: [], checkout_enabled: false, subscription_offer: { enabled: false },
      },
      '/api/v1/account/cosmetics/orders': { orders: [] },
      '/api/v1/account/keys': { keys: [] },
      '/api/v1/profile/acct-post-cutover': {
        account_id: ACCOUNT.id,
        public_username: account.public_username,
        bio: '',
        avatar_color: '#5edfff',
        shows_bots: false,
        bots: [],
      },
      '/api/v1/content': { blocks: {} },
      '/api/v1/service-status': {
        type: 'service_status', revision: 1, broadcast: null, maintenance: null,
      },
      '/api/v1/chat/config': { enabled: false },
      '/api/v1/version': { commit: 'browser-fixture', build_time: 'fixture' },
    };
    if (!(path in payloads)) throw new Error(`unexpected Dashboard API request: ${path}`);
    const payload = payloads[path];
    await route.fulfill({ json: payload });
  });
}

for (const prefix of ['', '/arena']) {
test(`central public username drives Dashboard controls at ${prefix || '/'}`, async ({ page }) => {
  const requested = new Set();
  await installDashboardRoutes(page, requested);

  await page.goto(`${prefix}/dashboard/`, { waitUntil: 'domcontentloaded' });

  await expect(page.locator('[data-tab="cosmetics"]')).toBeVisible();
  await expect(page.locator('[data-tab="profile"]')).toBeVisible();
  await expect(page.locator('#accountLogoutBtn')).toBeVisible();
  await expect(page.locator('#accountToolbarIdentity')).toContainText('arena_pilot');
  await expect(page.locator('#accountCosmeticsPanel')).toContainText('arena_pilot');
  await expect(page.locator('#botSwitcher')).toContainText('arena_pilot');
  await expect.poll(() => requested.has(`${prefix}/api/v1/account/cosmetics`)).toBe(true);

  await page.locator('[data-tab="profile"]').click();
  await expect(page.locator('#accountProfilePanel')).toContainText('arena_pilot');
  await expect.poll(() => requested.has(`${prefix}/api/v1/profile/acct-post-cutover`)).toBe(true);
  await expect(page.locator('#accountProfilePanel')).not.toContainText('Private Real Name');
  await expect(page.locator('#profileDisplayNameInput')).toHaveCount(0);
  await expect(page.getByRole('link', {name: 'Manage username in Angel Accounts'})).toHaveAttribute('href', 'https://accounts.angel-serv.com/portal/account/details');
  await expect(page.getByRole('button', {name: 'Refresh username'})).toBeVisible();
});

test(`missing public username offers central setup at ${prefix || '/'}`, async ({page}) => {
  const requested = new Set();
  await installDashboardRoutes(page, requested, null);
  await page.goto(`${prefix}/dashboard/`, {waitUntil: 'domcontentloaded'});
  await expect(page.locator('#accountToolbarIdentity')).toContainText('Username unavailable');
  await expect(page.locator('#accountToolbarIdentity')).not.toContainText('Private Real Name');
  await page.locator('[data-tab="profile"]').click();
  await expect(page.locator('#accountProfilePanel')).toContainText('Username unavailable');
  await expect(page.getByRole('link', {name:'Choose a username in Angel Accounts'})).toHaveAttribute('href','https://accounts.angel-serv.com/portal/account/details');
  await expect(page.locator('#profileBioInput')).toBeEditable();
  await expect(page.getByRole('button', {name:'Save profile'})).toBeEnabled();
});

}
