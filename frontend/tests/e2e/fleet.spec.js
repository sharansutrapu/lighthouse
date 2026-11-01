import { test, expect } from '@playwright/test';

const password = 'Integration-test-password-2026';

test.beforeEach(async ({ page, request, baseURL }) => {
  const headers = { 'X-LightHouse-Client': 'web', Origin: baseURL };
  let response = await request.post(`${baseURL}/api/token`, {
    headers,
    form: { username: 'admin', password },
  });
  if (response.status() === 401) {
    const initialLogin = await request.post(`${baseURL}/api/token`, {
      headers,
      form: { username: 'admin', password: 'admin123' },
    });
    expect(initialLogin.status()).toBe(200);
    const { access_token: initialToken } = await initialLogin.json();
    const changed = await request.post(`${baseURL}/api/user/change-password`, {
      headers: { ...headers, Authorization: `Bearer ${initialToken}` },
      form: { password },
    });
    expect(changed.status()).toBe(200);
    response = await request.post(`${baseURL}/api/token`, { headers, form: { username: 'admin', password } });
  }
  expect(response.status()).toBe(200);
  const { access_token: token } = await response.json();
  expect(token).toBeTruthy();
  await page.addInitScript((accessToken) => {
    localStorage.setItem('dl_token', btoa(encodeURIComponent(accessToken)));
    localStorage.setItem('dl_theme', btoa(encodeURIComponent('light')));
    window.__integrationErrors = [];
    window.addEventListener('error', (event) => window.__integrationErrors.push(event.message));
    const warn = console.warn;
    console.warn = (...arguments_) => {
      if (String(arguments_[0]).includes('[Vue warn]')) window.__integrationErrors.push(arguments_.map(String).join(' '));
      warn(...arguments_);
    };
  }, token);
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }));
});

test.afterEach(async ({ page }) => {
  expect(await page.evaluate(() => window.__integrationErrors || [])).toEqual([]);
});

for (const path of ['/dashboard', '/containers', '/images', '/volumes', '/networks', '/scans', '/gitops', '/health', '/admin', '/audit', '/logs', '/shell']) {
  test(`renders ${path} without errors or overflow`, async ({ page }, testInfo) => {
    const endpoints = {
      '/dashboard': '/api/containers', '/containers': '/api/containers', '/images': '/api/images',
      '/volumes': '/api/volumes', '/networks': '/api/networks', '/scans': '/api/containers',
      '/gitops': '/api/gitops/projects', '/health': '/api/system/history', '/admin': '/api/admin/users',
      '/audit': '/api/admin/audit', '/logs': '/api/containers', '/shell': '/api/containers',
    };
    const dataResponse = page.waitForResponse(response => new URL(response.url()).pathname === endpoints[path]);
    await page.goto(path);
    expect((await dataResponse).status()).toBe(200);
    await expect(page).not.toHaveURL(/\/login/);
    await expect(page.locator('.main-header')).toBeVisible();
    if (path === '/shell') await expect(page).toHaveURL(/\/containers$/);
    await expect(page.locator('#app')).not.toContainText('NaN');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    const logos = page.locator('.main-header').locator('img');
    for (const logo of await logos.all()) {
      if (await logo.isVisible()) expect(await logo.evaluate(image => image.complete && image.naturalWidth > 0)).toBe(true);
    }
    await page.screenshot({ path: testInfo.outputPath(`${path.slice(1)}.png`), fullPage: true });
  });
}

test('node navigation matches the deployment mode', async ({ page }, testInfo) => {
  await page.goto('/nodes');
  if (testInfo.project.metadata.mode === 'hub') {
    await expect(page).toHaveURL(/\/nodes$/);
    await expect(page.locator('#app')).toContainText('spoke-one-test');
    await expect(page.locator('#app')).toContainText('spoke-two-test');
  } else {
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.getByRole('link', { name: 'Nodes', exact: true })).toHaveCount(0);
  }
});

test('real login form authenticates and loads inventory', async ({ page }) => {
  await page.goto('/login');
  await page.getByPlaceholder('Enter username').fill('admin');
  await page.locator('input[type="password"]').fill(password);
  await page.getByRole('button', { name: 'Access Dashboard', exact: true }).click();
  await expect(page).toHaveURL(/\/dashboard/);
  await page.goto('/containers');
  await expect(page.locator('#app')).toContainText('verify-hub-web');
});

test('container inspection and log stream use the selected node', async ({ page }, testInfo) => {
  await page.goto('/containers');
  const targetName = testInfo.project.metadata.mode === 'hub' ? 'verify-one-web' : 'verify-hub-web';
  const inventory = await page.evaluate(async () => {
    const token = decodeURIComponent(atob(localStorage.getItem('dl_token')));
    const response = await fetch('/api/containers', { headers: { Authorization: `Bearer ${token}`, 'X-LightHouse-Client': 'web' } });
    return response.json();
  });
  const target = inventory.find(container => container.name === targetName);
  expect(target).toBeTruthy();
  await page.goto(`/containers/${target.id}`);
  await expect(page.locator('h1')).toHaveText(targetName);
  await expect(page.locator('#app')).not.toContainText('must-not-leak');
  await page.goto(`/logs?c=${target.id}`);
  await expect(page.locator('.stream-status')).toHaveText('Live');
  await expect(page.locator('.log-content')).toContainText('integration stdout');
  await expect(page.locator('.log-content')).toContainText('integration stderr');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
});

test('node filter and theme controls stay usable', async ({ page }, testInfo) => {
  await page.goto('/containers');
  await expect(page.locator('#app')).toContainText('verify-hub-web');
  if (testInfo.project.metadata.mode === 'hub') {
    await page.getByRole('combobox', { name: 'Node', exact: true }).selectOption('spoke-one-test');
    await expect(page.locator('tbody')).toContainText('verify-one-web');
    await expect(page.locator('tbody')).not.toContainText('verify-two-database');
    await expect(page.locator('tbody')).not.toContainText('verify-hub-web');
  }
  const toggle = page.getByRole('button', { name: 'Switch to dark mode', exact: true });
  await expect(toggle).toBeVisible();
  const bounds = await toggle.boundingBox();
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(testInfo.project.use.viewport.width);
  await toggle.click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('interactive shell waits for permissions and executes a command', async ({ page }, testInfo) => {
  await page.goto('/login');
  const inventory = await page.evaluate(async () => {
    const token = decodeURIComponent(atob(localStorage.getItem('dl_token')));
    const response = await fetch('/api/containers', { headers: { Authorization: `Bearer ${token}`, 'X-LightHouse-Client': 'web' } });
    return response.json();
  });
  const targetName = testInfo.project.metadata.mode === 'hub' ? 'verify-one-web' : 'verify-hub-web';
  const target = inventory.find(container => container.name === targetName);
  expect(target).toBeTruthy();
  let releaseProfile;
  const profileReady = new Promise(resolve => { releaseProfile = resolve; });
  await page.route('**/api/user/me', async route => {
    const response = await route.fetch();
    await profileReady;
    await route.fulfill({ response });
  });
  const received = [];
  page.on('websocket', socket => {
    if (socket.url().includes('/ws/shell/')) socket.on('framereceived', frame => received.push(String(frame.payload)));
  });
  const inventoryLoaded = page.waitForResponse(response => new URL(response.url()).pathname === '/api/containers');
  await page.goto(`/shell?c=${target.id}`);
  await inventoryLoaded;
  releaseProfile();
  await expect(page.locator('.xterm-screen')).toBeVisible();
  await page.locator('.xterm-screen').click();
  await page.keyboard.type("printf '\\n__browser_shell_verified__\\n'");
  await page.keyboard.press('Enter');
  await expect.poll(() => received.join('')).toContain('\r\n__browser_shell_verified__\r\n');
});