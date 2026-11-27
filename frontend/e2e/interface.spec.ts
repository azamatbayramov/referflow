import { randomBytes } from 'node:crypto'
import { expect, test, type Page, type BrowserContext } from '@playwright/test'
import { manage } from './helpers'
async function setup(
  page: Page,
  context: BrowserContext,
  baseURL: string,
  first = false,
) {
  const login = `interface-${randomBytes(8).toString('hex')}`,
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
    page.getByRole('heading', { name: 'Купоны', exact: true }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const response = await context.request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {
      discount_type: 'percent',
      value: '10',
      first_visit_only: first,
      total_limit: first ? 1 : null,
    },
  })
  expect(response.status()).toBe(201)
  return { coupon: await response.json(), headers }
}
async function open(page: Page, code: string) {
  await page.getByLabel('Номер купона', { exact: true }).fill(code)
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page
    .getByRole('button', { name: new RegExp(`№ ${code}.*Открыть`) })
    .click()
}
test('inline client survives lost response and section switches with one creation key', async ({
  page,
  context,
  baseURL,
}, info) => {
  const { coupon, headers } = await setup(page, context, baseURL!, true)
  await page.screenshot({
    path: info.outputPath('workspace.png'),
    fullPage: true,
  })
  await open(page, coupon.code)
  const form = page.getByRole('region', {
    name: 'Использовать купон',
    exact: true,
  })
  await form
    .getByRole('button', { name: '+ Добавить клиента здесь', exact: true })
    .click()
  const quick = page.getByRole('region', {
    name: 'Новый получатель скидки',
    exact: true,
  })
  await quick.getByLabel('Имя / ФИО', { exact: true }).fill('Новый Тестовый')
  let first = true
  const keys: string[] = []
  await page.route('**/api/clients', async (route) => {
    keys.push(route.request().headers()['idempotency-key'])
    if (first) {
      first = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await quick
    .getByRole('button', { name: 'Сохранить и выбрать', exact: true })
    .click()
  await expect(
    quick.getByRole('button', {
      name: 'Повторить сохранение клиента',
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: '← Назад к купонам', exact: true }),
  ).toBeDisabled()
  await page
    .getByRole('link', { name: 'Referflow — главная', exact: true })
    .click()
  await expect(
    quick.getByRole('button', {
      name: 'Повторить сохранение клиента',
      exact: true,
    }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Отправить', exact: true }).click()
  await page.getByRole('button', { name: 'Использовать', exact: true }).click()
  await expect(
    quick.getByRole('button', {
      name: 'Повторить сохранение клиента',
      exact: true,
    }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Статистика', exact: true }).click()
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await quick
    .getByRole('button', { name: 'Повторить сохранение клиента', exact: true })
    .click()
  await expect(
    form.getByLabel('Получатель скидки', { exact: true }),
  ).not.toHaveValue('')
  expect(keys).toHaveLength(2)
  expect(keys[0]).toBe(keys[1])
  const matches = await (
    await context.request.post('/api/clients/search', {
      headers,
      data: { query: 'Новый Тестовый' },
    })
  ).json()
  expect(matches.items).toHaveLength(1)
  const single = form.getByLabel(
    'Подтверждаю: на этот фактический прием применяется один купон',
    { exact: true },
  )
  const firstVisit = form.getByLabel(
    'Подтверждаю: это первое посещение этого специалиста с учетом прежних приемов',
    { exact: true },
  )
  await expect(single).not.toBeChecked()
  await expect(firstVisit).not.toBeChecked()
  await single.check()
  await firstVisit.check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  await expect(firstVisit).not.toBeChecked()
  await page.screenshot({
    path: info.outputPath('use-coupon.png'),
    fullPage: true,
  })
  const uses = await (
    await context.request.post('/api/redemptions/search', {
      headers,
      data: { coupon_id: coupon.id },
    })
  ).json()
  expect(uses.items).toHaveLength(1)

  // Commit cancellation on the server, hold its response, and switch tabs.
  // The mounted history must still refresh the coupon after the response.
  await expect(
    form.getByRole('button', {
      name: 'Подтвердить использование',
      exact: true,
    }),
  ).toBeDisabled()
  let release!: () => void
  let persisted!: () => void
  const hold = new Promise<void>((resolve) => {
    release = resolve
  })
  const saved = new Promise<void>((resolve) => {
    persisted = resolve
  })
  await page.route('**/api/redemptions/*/cancel', async (route) => {
    const response = await route.fetch()
    persisted()
    await hold
    await route.fulfill({ response })
  })
  await page.getByRole('button', { name: 'История', exact: true }).click()
  const history = page.getByRole('region', {
    name: 'История этого купона',
    exact: true,
  })
  await history
    .getByRole('button', { name: 'Отменить использование', exact: true })
    .click()
  await history
    .getByLabel('Причина отмены использования', { exact: true })
    .fill('Проверка перехода между вкладками')
  await history
    .getByRole('button', { name: 'Подтвердить отмену', exact: true })
    .click()
  await saved
  try {
    await page
      .getByRole('button', { name: 'Использовать', exact: true })
      .click()
    await expect(
      page.getByRole('button', { name: '← Назад к купонам', exact: true }),
    ).toBeDisabled()
  } finally {
    release()
  }
  await expect(
    form.getByRole('button', {
      name: 'Подтвердить использование',
      exact: true,
    }),
  ).toBeEnabled()
  await page
    .getByRole('button', { name: '← Назад к купонам', exact: true })
    .click()
  await expect(page.getByLabel('Номер купона', { exact: true })).toHaveValue(
    coupon.code,
  )
  await page.setViewportSize({ width: 320, height: 800 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth))
    .toBeLessThanOrEqual(320)
})
test('inline duplicate search selects an existing client without creating another card', async ({
  page,
  context,
  baseURL,
}) => {
  const { coupon, headers } = await setup(page, context, baseURL!)
  const existing = await (
    await context.request.post('/api/clients', {
      headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
      data: { name: 'Существующий Тестовый', phone: '+7 900 000-00-01' },
    })
  ).json()
  await open(page, coupon.code)
  const form = page.getByRole('region', {
    name: 'Использовать купон',
    exact: true,
  })
  await form
    .getByRole('button', { name: '+ Добавить клиента здесь', exact: true })
    .click()
  const quick = page.getByRole('region', {
    name: 'Новый получатель скидки',
    exact: true,
  })
  await quick.getByLabel('Имя / ФИО', { exact: true }).fill('Другое имя')
  await quick
    .getByLabel('Телефон (необязательно)', { exact: true })
    .fill('79000000001')
  await quick
    .getByRole('button', { name: 'Сохранить и выбрать', exact: true })
    .click()
  await expect(
    quick.getByRole('heading', { name: 'Похожие карточки', exact: true }),
  ).toBeVisible()
  await expect(
    quick.getByLabel('Это отдельный клиент, сохранить карточку'),
  ).not.toBeChecked()
  await quick.getByRole('button', { name: /Существующий Тестовый/ }).click()
  await expect(
    form.getByLabel('Получатель скидки', { exact: true }),
  ).toHaveValue(existing.id)
  const found = await (
    await context.request.post('/api/clients/search', { headers, data: {} })
  ).json()
  expect(found.items).toHaveLength(1)
})
