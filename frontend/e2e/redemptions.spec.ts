import { randomBytes } from 'node:crypto'
import { expect, test, type Page, type BrowserContext } from '@playwright/test'
import { manage } from './helpers'
async function setup(
  page: Page,
  context: BrowserContext,
  baseURL: string,
  firstVisitOnly: boolean,
  total: number | null,
) {
  const login = `usage-${randomBytes(8).toString('hex')}`,
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
  await expect(
    page.getByRole('heading', { name: 'Карточки клиентов' }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const client = await (
    await context.request.post('/api/clients', {
      headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
      data: { name: 'Получатель Тестовый' },
    })
  ).json()
  const coupon = await (
    await context.request.post('/api/coupons', {
      headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
      data: {
        discount_type: 'percent',
        value: '12.50',
        total_limit: total,
        per_client_limit: 1,
        first_visit_only: firstVisitOnly,
        restrictions: 'Тестовые условия для ручной проверки',
      },
    })
  ).json()
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page.getByLabel('Номер купона', { exact: true }).fill(coupon.code)
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page
    .getByRole('button', { name: new RegExp(`№ ${coupon.code}.*Открыть`) })
    .click()
  const form = page.getByRole('region', {
    name: 'Использовать купон',
    exact: true,
  })
  await form
    .getByLabel('Найти получателя скидки', { exact: true })
    .fill('Получатель Тестовый')
  await form.getByRole('button', { name: 'Найти клиента', exact: true }).click()
  await expect(form.locator('#redemption-client option')).toHaveCount(2)
  await form
    .getByLabel('Получатель скидки', { exact: true })
    .selectOption(client.id)
  return { client, coupon, headers, form }
}
const single = 'Подтверждаю: на этот фактический прием применяется один купон'
const first =
  'Подтверждаю: это первое посещение этого специалиста с учетом прежних приемов'
test('confirm manually, apply, cancel, replace and retain client/coupon audit after revocation', async ({
  page,
  context,
  baseURL,
}) => {
  const { client, coupon, headers, form } = await setup(
    page,
    context,
    baseURL!,
    true,
    1,
  )
  await expect(form.getByLabel(single, { exact: true })).not.toBeChecked()
  await expect(form.getByLabel(first, { exact: true })).not.toBeChecked()
  await form.getByLabel(single, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  const empty = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { client_id: client.id },
    })
  ).json()
  expect(empty.items).toHaveLength(0)
  await form.getByLabel(first, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  await expect(
    form.getByRole('button', {
      name: 'Подтвердить использование',
      exact: true,
    }),
  ).toBeDisabled()
  const history = page.getByRole('region', {
    name: 'История этого купона',
    exact: true,
  })
  await expect(
    history.getByText('Получатель Тестовый', { exact: false }),
  ).toBeVisible()
  await history
    .getByRole('button', { name: 'Отменить использование', exact: true })
    .click()
  await history
    .getByLabel('Причина отмены использования', { exact: true })
    .fill('Ошибочный выбор: тест')
  await history
    .getByRole('button', { name: 'Подтвердить отмену', exact: true })
    .click()
  await expect(
    form.getByRole('button', {
      name: 'Подтвердить использование',
      exact: true,
    }),
  ).toBeEnabled()
  await expect(form.getByLabel(first, { exact: true })).not.toBeChecked()
  await form.getByLabel(single, { exact: true }).check()
  await form.getByLabel(first, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  await page
    .getByLabel('Причина отзыва', { exact: true })
    .fill('Отзыв после использования: тест')
  await page
    .getByLabel('Понимаю, что отзыв окончательный', { exact: true })
    .check()
  await page
    .getByRole('button', { name: 'Отозвать купон', exact: true })
    .click()
  await expect(
    page.getByText('Купон окончательно отозван.', { exact: true }),
  ).toBeVisible()
  await history
    .getByRole('button', { name: 'Отменить использование', exact: true })
    .click()
  await history
    .getByLabel('Причина отмены использования', { exact: true })
    .fill('Отмена после отзыва: тест')
  await history
    .getByRole('button', { name: 'Подтвердить отмену', exact: true })
    .click()
  await expect(
    history.getByRole('button', {
      name: 'Отменить использование',
      exact: true,
    }),
  ).toHaveCount(0)
  const detail = await (
    await context.request.get(`/api/coupons/${coupon.id}`)
  ).json()
  expect(detail.coupon.status).toBe('revoked')
  expect(detail.coupon.used_count).toBe(0)
  const audit = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { client_id: client.id },
    })
  ).json()
  expect(audit.items).toHaveLength(2)
  expect(
    audit.items.every(
      (u: {
        canceled_at: string
        first_visit_confirmed: boolean
        value: string
      }) => u.canceled_at && u.first_visit_confirmed && u.value === '12.50',
    ),
  ).toBe(true)
  await page.getByRole('button', { name: 'Клиенты', exact: true }).click()
  await page
    .getByRole('button', { name: /Получатель Тестовый.*Открыть/ })
    .click()
  const clientHistory = page.getByRole('region', {
    name: 'Использования купонов клиента',
    exact: true,
  })
  await expect(
    clientHistory.getByText('Отменено', { exact: true }),
  ).toHaveCount(2)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
})
test('lost redemption response can be retried after cancellation without resurrection', async ({
  page,
  context,
  baseURL,
}) => {
  const { client, coupon, headers, form } = await setup(
    page,
    context,
    baseURL!,
    false,
    null,
  )
  let firstRequest = true
  await page.route('**/api/redemptions', async (route) => {
    if (firstRequest) {
      firstRequest = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await form.getByLabel(single, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByRole('button', { name: 'Повторить подтверждение', exact: true }),
  ).toBeVisible()
  const recorded = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { client_id: client.id },
    })
  ).json()
  expect(recorded.items).toHaveLength(1)
  const cancel = await context.request.post(
    `/api/redemptions/${recorded.items[0].id}/cancel`,
    { headers, data: { reason: 'Отмена перед повтором: тест' } },
  )
  expect(cancel.status()).toBe(200)
  await form
    .getByRole('button', { name: 'Повторить подтверждение', exact: true })
    .click()
  await expect(
    form.getByText(
      'Это использование уже отменено. Новая операция не создана.',
      { exact: true },
    ),
  ).toBeVisible()
  const history = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { coupon_id: coupon.id },
    })
  ).json()
  expect(history.items).toHaveLength(1)
  expect(history.items[0].canceled_at).not.toBeNull()
  const detail = await (
    await context.request.get(`/api/coupons/${coupon.id}`)
  ).json()
  expect(detail.coupon.used_count).toBe(0)
  await form.getByLabel(single, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  const second = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { coupon_id: coupon.id },
    })
  ).json()
  expect(second.items).toHaveLength(2)
  expect(
    second.items.filter((u: { canceled_at: string | null }) => !u.canceled_at),
  ).toHaveLength(1)
})
