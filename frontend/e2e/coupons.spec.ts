import { randomBytes, randomInt } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { manage } from './helpers'
async function login(page: Page) {
  const login = `coupons-${randomBytes(8).toString('hex')}`,
    password = randomBytes(12).toString('base64url')
  manage(
    [
      'create-user',
      '--login',
      login,
      '--name',
      'Тестовый специалист',
      '--password-stdin',
    ],
    password,
  )
  await page.goto('/')
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Купоны', exact: true }),
  ).toBeVisible()
}
test('issue percent coupon, find manual code, inspect terms and permanently revoke', async ({
  page,
  context,
  baseURL,
}) => {
  await login(page)
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL!).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const clientResponse = await context.request.post('/api/clients', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { name: 'Тестовый Рекомендатель' },
  })
  const client = await clientResponse.json()
  const code = String(randomInt(1000000)).padStart(6, '0')
  await page.getByRole('button', { name: 'Создать купон', exact: true }).click()
  await page
    .getByLabel('Номер на бумаге (необязательно)')
    .fill(`${code.slice(0, 3)}-${code.slice(3)}`)
  await page.getByLabel('Скидка, %', { exact: true }).fill('12,50')
  await page.getByLabel('Без общего лимита', { exact: true }).uncheck()
  await page.getByLabel('Всего использований', { exact: true }).fill('5')
  await page
    .getByLabel('Только первое посещение специалиста', { exact: true })
    .check()
  await page.getByLabel('Указать срок действия', { exact: true }).check()
  await page
    .getByLabel('Последний день действия', { exact: true })
    .fill('2099-12-31')
  await page
    .getByLabel('Ограничения по услугам (необязательно)')
    .fill('Тестовое ограничение\nВторая строка без обрезки')
  await page.getByText('Кому выдан (необязательно)', { exact: true }).click()
  await page
    .getByLabel('Найти клиента', { exact: true })
    .fill('Тестовый Рекомендатель')
  await page.getByRole('button', { name: 'Искать', exact: true }).click()
  await expect(page.locator('#referrer-client option')).toHaveCount(2)
  await page
    .getByLabel('Кому выдан купон', { exact: true })
    .selectOption(client.id)
  await page
    .getByRole('button', { name: 'Сохранить купон', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: `Купон № ${code}`, exact: true }),
  ).toBeVisible()
  await expect(page.getByText('Купон создан.', { exact: true })).toBeVisible()
  await expect(page.getByText('12,50 %', { exact: true })).toBeVisible()
  await expect(
    page.getByText('До 31.12.2099 включительно (МСК, UTC+3)', { exact: true }),
  ).toBeVisible()
  await expect(
    page.getByText('Только первое посещение', { exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).click()
  await page.getByLabel('Номер купона', { exact: true }).fill(code)
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page
    .getByRole('button', { name: new RegExp(`№ ${code}.*Открыть`) })
    .click()
  await page
    .getByLabel('Причина отзыва', { exact: true })
    .fill('Ошибка выдачи: тест')
  await page
    .getByLabel('Понимаю, что отзыв окончательный', { exact: true })
    .check()
  await page
    .getByRole('button', { name: 'Отозвать купон', exact: true })
    .click()
  await expect(
    page.getByText('Купон окончательно отозван.', { exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Отозвать купон', exact: true }),
  ).toHaveCount(0)
  const search = await (
    await context.request.post('/api/coupons/search', {
      headers,
      data: { code },
    })
  ).json()
  expect(search.items).toHaveLength(1)
  expect(search.items[0].status).toBe('revoked')
  expect(search.items[0].used_count).toBe(0)
  expect(search.items[0].referrer_client_id).toBe(client.id)
  const taken = await context.request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { code, discount_type: 'fixed', value: '10' },
  })
  expect(taken.status()).toBe(409)
  expect((await taken.json()).status).toBe('code_taken')
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})
test('fixed perpetual coupon survives lost response with independent unlimited budgets', async ({
  page,
  context,
  baseURL,
}) => {
  await login(page)
  let first = true
  await page.route('**/api/coupons', async (route) => {
    if (first) {
      first = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page.getByRole('button', { name: 'Создать купон', exact: true }).click()
  await page.getByLabel('Вид скидки', { exact: true }).selectOption('fixed')
  await page.getByLabel('Скидка, ₽', { exact: true }).fill('350,25')
  await page.getByLabel('Без лимита на клиента', { exact: true }).check()
  await page
    .getByRole('button', { name: 'Сохранить купон', exact: true })
    .click()
  await expect(
    page.getByRole('button', { name: 'Повторить создание', exact: true }),
  ).toBeVisible()
  await page
    .getByRole('button', { name: 'Повторить создание', exact: true })
    .click()
  await expect(page.getByText('Купон создан.', { exact: true })).toBeVisible()
  await expect(page.getByText('350,25 ₽', { exact: true })).toBeVisible()
  await expect(page.getByText('Бессрочно', { exact: true })).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL!).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const result = await (
    await context.request.post('/api/coupons/search', { headers, data: {} })
  ).json()
  expect(result.items).toHaveLength(1)
  const coupon = result.items[0]
  expect(coupon.code).toMatch(/^\d{6}$/)
  expect(coupon.id).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  )
  expect(coupon.total_limit).toBeNull()
  expect(coupon.per_client_limit).toBeNull()
  expect(coupon.expires_at).toBeNull()
})
