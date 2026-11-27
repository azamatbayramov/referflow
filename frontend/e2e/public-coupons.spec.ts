import { randomBytes } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from '@playwright/test'
import type jsQR from 'jsqr'
import { manage } from './helpers'

declare global {
  interface Window {
    jsQR: typeof jsQR
  }
}
const require = createRequire(import.meta.url)
async function specialist(request: APIRequestContext, baseURL: string) {
  const login = `public-${randomBytes(8).toString('hex')}`
  const password = randomBytes(12).toString('base64url')
  manage(
    [
      'create-user',
      '--login',
      login,
      '--name',
      'Закрытое имя специалиста',
      '--password-stdin',
    ],
    password,
  )
  const sessionResponse = await request.post('/api/auth/login', {
    headers: { Origin: new URL(baseURL).origin },
    data: { login, password },
  })
  expect(sessionResponse.status()).toBe(200)
  const session = await sessionResponse.json()
  const headers = {
    Origin: new URL(baseURL).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const clientResponse = await request.post('/api/clients', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { name: 'Закрытое имя клиента', phone: '+7 900 000-00-01' },
  })
  expect(clientResponse.status()).toBe(201)
  return { headers, client: await clientResponse.json(), login, password }
}
async function decodeQR(page: Page, png?: string) {
  await page.addScriptTag({ path: require.resolve('jsqr') })
  return page.evaluate(async (data) => {
    const img = data
      ? new Image()
      : document.querySelector<HTMLImageElement>('.public-qr')!
    if (data) img.src = data
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = data ? img.naturalWidth : 400
    canvas.height = data ? img.naturalHeight : 400
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height)
    return window.jsQR(pixels.data, pixels.width, pixels.height)?.data
  }, png)
}
test('public QR, image and paper share live terms without clients or consumption', async ({
  page,
  request,
  baseURL,
}, info) => {
  const { headers, client, login, password } = await specialist(
    request,
    baseURL!,
  )
  const restrictions =
    'Только тестовая консультация.\nУсловия полностью, <b>без HTML</b>.'
  const created = await request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {
      discount_type: 'percent',
      value: '12.50',
      total_limit: 1,
      first_visit_only: true,
      expires_on: '2099-12-31',
      restrictions,
      referrer_client_id: client.id,
    },
  })
  expect(created.status()).toBe(201)
  const coupon = await created.json()
  const calls: string[] = []
  page.on('request', (r) => {
    if (new URL(r.url()).pathname.startsWith('/api/'))
      calls.push(new URL(r.url()).pathname)
  })
  await page.goto(`/c/${coupon.id}?source=paper#private-fragment`)
  const card = page.getByRole('article')
  await expect(
    card.getByRole('heading', { name: `Купон № ${coupon.code}` }),
  ).toBeVisible()
  await expect(card.getByText('12,50 %', { exact: true })).toBeVisible()
  await expect(
    card.getByText('Только первое посещение специалиста', { exact: true }),
  ).toBeVisible()
  await expect(
    card.getByText('До 31.12.2099 включительно (МСК, UTC+3)', { exact: true }),
  ).toBeVisible()
  await expect(card.locator('.public-restrictions p')).toHaveText(restrictions)
  await expect(card.locator('.public-restrictions b')).toHaveCount(0)
  expect(new Set(calls)).toEqual(new Set([`/api/public/coupons/${coupon.id}`]))
  const link = `${new URL(baseURL!).origin}/c/${coupon.id}`
  expect(await decodeQR(page)).toBe(link)
  await page.screenshot({
    path: info.outputPath('public-coupon.png'),
    fullPage: true,
  })
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          document.body.dataset.copied = text
        },
      },
    })
  })
  await page
    .getByRole('button', { name: 'Скопировать ссылку', exact: true })
    .click()
  await expect(page.locator('body')).toHaveAttribute('data-copied', link)
  await expect(
    page.getByText('Ссылка скопирована. Ее можно отправить клиенту.', {
      exact: true,
    }),
  ).toBeVisible()
  const body = await page.locator('body').innerText()
  for (const privateText of [
    client.id,
    client.name,
    client.phone,
    coupon.id,
    'Закрытое имя специалиста',
    'Причина отзыва',
  ])
    expect(body).not.toContain(privateText)
  await expect(
    page.getByRole('button', {
      name: 'Подтвердить использование',
      exact: true,
    }),
  ).toHaveCount(0)
  expect((await page.request.get(`/api/coupons/${coupon.id}`)).status()).toBe(
    401,
  )
  expect(
    (
      await page.request.post('/api/redemptions', {
        data: { coupon_id: coupon.id, client_id: client.id },
      })
    ).status(),
  ).toBe(401)
  const publicAPI = await page.request.get(`/api/public/coupons/${coupon.id}`)
  expect(publicAPI.headers()['cache-control']).toBe('no-store')
  expect(publicAPI.headers()['referrer-policy']).toBe('no-referrer')
  expect(Object.keys(await publicAPI.json()).sort()).toEqual([
    'code',
    'discount_type',
    'expires_at',
    'expires_on',
    'first_visit_only',
    'per_client_limit',
    'restrictions',
    'status',
    'total_limit',
    'value',
  ])
  if (!process.env.E2E_MANAGE_BIN) {
    const html = await page.request.get(`/c/${coupon.id}`)
    expect(html.headers()['cache-control']).toBe('no-store')
    expect(html.headers()['referrer-policy']).toBe('no-referrer')
    expect(html.headers()['x-robots-tag']).toContain('noindex')
  }
  const downloadEvent = page.waitForEvent('download')
  await page
    .getByRole('button', { name: 'Сохранить купон', exact: true })
    .click()
  const download = await downloadEvent
  expect(download.suggestedFilename()).toBe(`referflow-${coupon.code}.png`)
  const png = readFileSync((await download.path())!)
  expect(
    await decodeQR(page, `data:image/png;base64,${png.toString('base64')}`),
  ).toBe(link)
  await page.evaluate(() => {
    window.print = () => {
      document.body.dataset.printed = 'yes'
    }
  })
  await page.locator('.public-extra > summary').click()
  await page.getByRole('button', { name: 'Распечатать', exact: true }).click()
  await expect(page.locator('body')).toHaveAttribute('data-printed', 'yes')
  await page.emulateMedia({ media: 'print' })
  await expect(
    page.getByRole('button', { name: 'Распечатать', exact: true }),
  ).toBeHidden()
  await expect(card.locator('.public-qr')).toBeVisible()
  await expect(card.locator('.public-restrictions p')).toHaveText(restrictions)
  const paper = await page.pdf({
    path: info.outputPath('coupon.pdf'),
    preferCSSPageSize: true,
  })
  expect(paper.toString('latin1').match(/\/Type\s*\/Page\b/g)).toHaveLength(1)
  await page.emulateMedia({ media: 'screen' })
  for (let i = 0; i < 3; i++)
    await page
      .getByRole('button', { name: 'Обновить статус', exact: true })
      .click()
  const detail = await (await request.get(`/api/coupons/${coupon.id}`)).json()
  expect(detail.coupon.used_count).toBe(0)
  expect(detail.events).toHaveLength(1)
  const used = await request.post('/api/redemptions', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {
      coupon_id: coupon.id,
      client_id: client.id,
      first_visit_confirmed: true,
      single_coupon_confirmed: true,
    },
  })
  expect(used.status()).toBe(201)
  const redemption = await used.json()
  await page
    .getByRole('button', { name: 'Обновить статус', exact: true })
    .click()
  await expect(
    card.getByText('Исчерпан общий лимит', { exact: true }),
  ).toBeVisible()
  expect(
    (
      await request.post(`/api/redemptions/${redemption.id}/cancel`, {
        headers,
        data: { reason: 'Закрытая причина отмены' },
      })
    ).status(),
  ).toBe(200)
  await page.reload()
  await expect(card.getByText('Доступен', { exact: true })).toBeVisible()
  expect(
    (
      await request.post(`/api/coupons/${coupon.id}/revoke`, {
        headers,
        data: { reason: 'Закрытая причина отзыва' },
      })
    ).status(),
  ).toBe(200)
  await page.reload()
  await expect(card.getByText('Отозван', { exact: true })).toBeVisible()
  expect(await page.locator('body').innerText()).not.toContain(
    'Закрытая причина',
  )
  expect(
    (await (await request.get(`/api/coupons/${coupon.id}`)).json()).coupon
      .used_count,
  ).toBe(0)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  // Specialist reaches the same public document through the issuance card.
  await page.goto('/')
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page
    .getByRole('button', { name: new RegExp(`№ ${coupon.code}.*Открыть`) })
    .click()
  await page.getByRole('button', { name: 'Отправить', exact: true }).click()
  await expect(
    page.getByRole('link', { name: 'Открыть карточку для клиента' }),
  ).toHaveAttribute('href', `/c/${coupon.id}`)
})

test('unlimited fixed coupon keeps all long restrictions on mobile and paper', async ({
  page,
  request,
  baseURL,
}, info) => {
  const { headers } = await specialist(request, baseURL!)
  const restrictions = 'Начало условий\n' + 'О'.repeat(1900) + '\nКонец условий'
  const c = await (
    await request.post('/api/coupons', {
      headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
      data: {
        discount_type: 'fixed',
        value: '350.25',
        per_client_limit: null,
        restrictions,
      },
    })
  ).json()
  await page.goto(`/c/${c.id}`)
  await expect(
    page.getByRole('article').getByText('350,25 ₽', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText('Бессрочно', { exact: true })).toBeVisible()
  await expect(page.getByText('Без ограничения', { exact: true })).toHaveCount(
    2,
  )
  await expect(page.locator('.public-restrictions p')).toHaveText(restrictions)
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error('denied')
        },
      },
    })
  })
  await page
    .getByRole('button', { name: 'Скопировать ссылку', exact: true })
    .click()
  await expect(page.getByLabel('Ссылка на купон')).toHaveValue(
    `${new URL(baseURL!).origin}/c/${c.id}`,
  )
  await page.setViewportSize({ width: 320, height: 800 })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await page.emulateMedia({ media: 'print' })
  const text = page.locator('.public-restrictions p')
  await expect(text).toHaveText(restrictions)
  expect(await text.evaluate((el) => getComputedStyle(el).overflow)).toBe(
    'visible',
  )
  await page.pdf({
    path: info.outputPath('long-coupon.pdf'),
    preferCSSPageSize: true,
  })
})

test('expired, missing and unavailable public pages never show a stale usable coupon', async ({
  page,
  request,
  baseURL,
}) => {
  const { headers } = await specialist(request, baseURL!)
  const c = await (
    await request.post('/api/coupons', {
      headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
      data: { discount_type: 'percent', value: '10', expires_on: '2000-01-01' },
    })
  ).json()
  await page.goto(`/c/${c.id}`)
  await expect(
    page.getByRole('article').getByText('Истек срок', { exact: true }),
  ).toBeVisible()
  await page.locator('.public-extra > summary').click()
  await page.route('**/api/public/coupons/*', (route) =>
    route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: '{"status":"unavailable"}',
    }),
  )
  await page
    .getByRole('button', { name: 'Обновить статус', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Не удалось проверить купон' }),
  ).toBeVisible()
  await expect(page.getByRole('article')).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Распечатать', exact: true }),
  ).toHaveCount(0)
  await page.unroute('**/api/public/coupons/*')
  await page
    .getByRole('button', { name: 'Проверить снова', exact: true })
    .click()
  await expect(
    page.getByRole('article').getByText('Истек срок', { exact: true }),
  ).toBeVisible()
  await page.goto(`/c/${crypto.randomUUID()}`)
  await expect(
    page.getByRole('heading', { name: 'Купон не найден' }),
  ).toBeVisible()
  await page.goto('/c/123456')
  await expect(
    page.getByRole('heading', { name: 'Купон не найден' }),
  ).toBeVisible()
  await expect(page.getByLabel('Логин', { exact: true })).toHaveCount(0)
})
