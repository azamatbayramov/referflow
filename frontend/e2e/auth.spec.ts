import { execFileSync } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'

// Compose by default; local native PostgreSQL/API can use the same scenarios.
function manage(args: string[], input = '') {
  if (process.env.E2E_MANAGE_BIN) {
    execFileSync(process.env.E2E_MANAGE_BIN, args, {
      input,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
  } else {
    execFileSync(
      'docker',
      ['compose', 'exec', '-T', 'api', 'manage', ...args],
      {
        cwd: '..',
        input,
        stdio: ['pipe', 'pipe', 'pipe'],
      },
    )
  }
}
function expire(token: string) {
  const hash = createHash('sha256').update(token).digest('hex')
  const sql = `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE token_hash=decode('${hash}','hex');`
  if (process.env.E2E_PSQL_BIN) {
    execFileSync(process.env.E2E_PSQL_BIN, ['-v', 'ON_ERROR_STOP=1'], {
      input: sql,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
  } else {
    execFileSync(
      'docker',
      [
        'compose',
        'exec',
        '-T',
        'db',
        'sh',
        '-c',
        'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1',
      ],
      {
        cwd: '..',
        input: sql,
        stdio: ['pipe', 'pipe', 'pipe'],
      },
    )
  }
}

test('login, reload, CSRF rejection, logout and revoked-cookie rejection', async ({
  page,
  context,
  request,
  baseURL,
}) => {
  const email = `test-${randomBytes(8).toString('hex')}@example.test`
  const password = randomBytes(24).toString('base64url')
  manage(
    [
      'create-user',
      '--email',
      email,
      '--name',
      'Тестовый специалист',
      '--password-stdin',
    ],
    password,
  )
  const initial = await request.get('/api/auth/session')
  expect(initial.status()).toBe(401)
  await page.goto('/')
  await expect(
    page.getByRole('heading', { name: 'Вход в кабинет' }),
  ).toBeVisible()
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Пароль', { exact: true }).fill(`${password}wrong`)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Неверный email или пароль.')
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: /Здравствуйте/ }),
  ).toBeVisible()
  const cookie = (await context.cookies()).find((item) =>
    item.name.includes('referflow_session'),
  )!
  expect(cookie.httpOnly).toBe(true)
  expect(cookie.sameSite).toBe('Strict')
  expect(await page.evaluate(() => document.cookie)).not.toContain(cookie.value)
  expect(
    await page.evaluate(() => [localStorage.length, sessionStorage.length]),
  ).toEqual([0, 0])
  await page.reload()
  await expect(
    page.getByRole('heading', { name: /Здравствуйте/ }),
  ).toBeVisible()
  const before = await context.request.get(
    '/api/auth/session?specialist_id=other',
  )
  expect((await before.json()).user.email).toBe(email)
  const rejected = await context.request.post('/api/auth/logout', {
    headers: { Origin: new URL(baseURL!).origin },
  })
  expect(rejected.status()).toBe(403)
  await page.getByRole('button', { name: 'Выйти', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Вход в кабинет' }),
  ).toBeVisible()
  const replay = await request.get('/api/auth/session', {
    headers: { Cookie: `${cookie.name}=${cookie.value}` },
  })
  expect(replay.status()).toBe(401)
})

test('expired sessions stop API and browser access', async ({
  page,
  context,
}) => {
  const email = `expiry-${randomBytes(8).toString('hex')}@example.test`
  const password = randomBytes(24).toString('base64url')
  manage(
    [
      'create-user',
      '--email',
      email,
      '--name',
      'Проверка срока',
      '--password-stdin',
    ],
    password,
  )
  await page.goto('/')
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: /Здравствуйте/ }),
  ).toBeVisible()
  const cookie = (await context.cookies()).find((item) =>
    item.name.includes('referflow_session'),
  )!
  expire(cookie.value)
  expect((await context.request.get('/api/auth/session')).status()).toBe(401)
  await page.reload()
  await expect(
    page.getByRole('heading', { name: 'Вход в кабинет' }),
  ).toBeVisible()
})

test('login errors, untrusted origins and attempt limits', async ({
  request,
  baseURL,
}) => {
  const origin = new URL(baseURL!).origin
  const email = `limits-${randomBytes(8).toString('hex')}@example.test`
  const password = randomBytes(24).toString('base64url')
  const rejected = await request.post('/api/auth/login', {
    headers: { Origin: 'https://untrusted.example' },
    data: { email, password },
  })
  expect(rejected.status()).toBe(403)
  for (let index = 0; index < 10; index++) {
    const response = await request.post('/api/auth/login', {
      headers: { Origin: origin },
      data: { email, password },
    })
    expect(response.status()).toBe(401)
    expect(await response.json()).toEqual({ status: 'invalid_credentials' })
  }
  const limited = await request.post('/api/auth/login', {
    headers: { Origin: origin },
    data: { email, password },
  })
  expect(limited.status()).toBe(429)
  expect(limited.headers()['retry-after']).toBe('900')
})
