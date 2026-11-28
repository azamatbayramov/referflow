import { randomBytes, randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { manage } from './helpers'

test('dedicated redemption checks the recipient and preserves a lost confirmation across sections', async ({
  page,
  context,
  baseURL,
}, info) => {
  const login = `redeem-flow-${randomBytes(8).toString('hex')}`
  const password = randomBytes(12).toString('base64url')
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
  await page.getByRole('button', { name: 'Применить', exact: true }).click()
  const workspace = page.getByRole('region', {
    name: 'Применение купона',
    exact: true,
  })
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL!).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const clientResponse = await context.request.post('/api/clients', {
    headers: { ...headers, 'Idempotency-Key': randomUUID() },
    data: { name: 'Получатель Тестовый' },
  })
  expect(clientResponse.status()).toBe(201)
  const client = await clientResponse.json()
  const couponResponse = await context.request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': randomUUID() },
    data: {
      discount_type: 'percent',
      value: '20',
      first_visit_only: true,
      total_limit: 2,
      per_client_limit: 1,
      restrictions: 'Только консультация',
    },
  })
  expect(couponResponse.status()).toBe(201)
  const coupon = await couponResponse.json()
  const number = workspace.getByLabel('Номер купона для применения', {
    exact: true,
  })
  // Choose a valid but absent number rather than depending on random issuance.
  const absent = coupon.code === '000000' ? '000001' : '000000'
  await number.fill(absent)
  await workspace
    .getByRole('button', { name: 'Проверить купон', exact: true })
    .click()
  await expect(workspace.getByRole('alert')).toContainText('не найден')
  await number.fill(coupon.code)
  await workspace
    .getByRole('button', { name: 'Проверить купон', exact: true })
    .click()
  await expect(
    workspace.getByText('Купон доступен', { exact: true }),
  ).toBeVisible()
  await expect(
    workspace.getByText('Только консультация', { exact: true }).first(),
  ).toBeVisible()
  const single = workspace.getByLabel(
    'Подтверждаю: на этот фактический прием применяется один купон',
    { exact: true },
  )
  const first = workspace.getByLabel(
    'Подтверждаю: это первое посещение этого специалиста с учетом прежних приемов',
    { exact: true },
  )
  await expect(single).not.toBeVisible()
  await workspace
    .getByLabel('Найти получателя скидки', { exact: true })
    .fill('Получатель')
  await workspace.getByRole('button', { name: 'Найти', exact: true }).click()
  await workspace
    .getByLabel('Получатель скидки', { exact: true })
    .selectOption(client.id)
  await expect(single).toBeVisible()
  await expect(single).not.toBeChecked()
  await expect(first).not.toBeChecked()
  await page.screenshot({
    path: info.outputPath('redeem-recipient.png'),
    fullPage: true,
  })
  await single.check()
  await first.check()
  let lose = true
  const keys: string[] = []
  const bodies: unknown[] = []
  await page.route('**/api/redemptions', async (route) => {
    keys.push(route.request().headers()['idempotency-key'])
    bodies.push(route.request().postDataJSON())
    if (lose) {
      lose = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await workspace
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    workspace.getByRole('button', {
      name: 'Повторить подтверждение',
      exact: true,
    }),
  ).toBeVisible()
  await expect(number).toBeDisabled()
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await page.getByRole('button', { name: 'Применить', exact: true }).click()
  await workspace
    .getByRole('button', { name: 'Повторить подтверждение', exact: true })
    .click()
  await expect(
    workspace.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  expect(keys).toHaveLength(2)
  expect(keys[0]).toBe(keys[1])
  expect(bodies[0]).toEqual(bodies[1])
  await expect(workspace.getByRole('alert')).toContainText(
    'максимально допустимое',
  )
  await expect(single).not.toBeVisible()
  const detail = await (
    await context.request.get(`/api/coupons/${coupon.id}`)
  ).json()
  expect(detail.coupon.used_count).toBe(1)
  const history = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { coupon_id: coupon.id },
    })
  ).json()
  expect(history.items).toHaveLength(1)
  expect(history.items[0].client_id).toBe(client.id)
  const revoked = await context.request.post(
    `/api/coupons/${coupon.id}/revoke`,
    { headers, data: { reason: 'Тестовый отзыв' } },
  )
  expect(revoked.status()).toBe(200)
  await workspace
    .getByRole('button', { name: 'Проверить купон', exact: true })
    .click()
  await expect(
    workspace.getByText('Купон отозван', { exact: true }),
  ).toBeVisible()
  await expect(
    workspace.getByLabel('Найти получателя скидки', { exact: true }),
  ).not.toBeVisible()
  await page.setViewportSize({ width: 320, height: 800 })
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(320)
})
