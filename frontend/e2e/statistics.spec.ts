import { randomBytes } from 'node:crypto'
import { expect, test, type Page, type BrowserContext } from '@playwright/test'
import { manage } from './helpers'
const single = 'Подтверждаю: на этот фактический прием применяется один купон'
const first =
  'Подтверждаю: это первое посещение этого специалиста с учетом прежних приемов'
async function login(page: Page, context: BrowserContext, baseURL: string) {
  const login = `pilot-${randomBytes(8).toString('hex')}`,
    password = randomBytes(12).toString('base64url')
  manage(
    [
      'create-user',
      '--login',
      login,
      '--name',
      'Специалист пилота',
      '--password-stdin',
    ],
    password,
  )
  await page.goto('/')
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await page.getByRole('button', { name: 'Клиенты', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Карточки клиентов', exact: true }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  return { Origin: new URL(baseURL).origin, 'X-CSRF-Token': session.csrf_token }
}
async function statistics(
  page: Page,
  values: [number, number, number, number, number, number],
  share: RegExp,
) {
  await page.getByRole('button', { name: 'Статистика', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Статистика пилота', exact: true }),
  ).toBeVisible()
  for (const [i, key] of [
    'issued',
    'used',
    'active',
    'clients',
    'first',
    'canceled',
  ].entries()) {
    await expect(page.getByTestId(`stat-${key}`)).toHaveText(String(values[i]))
  }
  await expect(page.getByTestId('stat-share')).toHaveText(share)
}
async function chooseClient(page: Page) {
  await page.getByRole('button', { name: 'Использовать', exact: true }).click()
  const form = page.getByRole('region', {
    name: 'Использовать купон',
    exact: true,
  })
  await form
    .getByLabel('Найти получателя скидки', { exact: true })
    .fill('Клиент пилота')
  await form.getByRole('button', { name: 'Найти', exact: true }).click()
  await expect(form.locator('#redemption-client option')).toHaveCount(2)
  const id = await form
    .locator('#redemption-client option')
    .nth(1)
    .getAttribute('value')
  await form.getByLabel('Получатель скидки', { exact: true }).selectOption(id!)
  return form
}
async function cancelFirst(page: Page, reason: string) {
  await page.getByRole('button', { name: 'История', exact: true }).click()
  const history = page.getByRole('region', {
    name: 'История этого купона',
    exact: true,
  })
  await history
    .getByRole('button', { name: 'Отменить использование', exact: true })
    .first()
    .click()
  await history
    .getByLabel('Причина отмены использования', { exact: true })
    .fill(reason)
  await history
    .getByRole('button', { name: 'Подтвердить отмену', exact: true })
    .click()
  await expect(
    history.getByText('Использование отменено. Лимиты освобождены.', {
      exact: true,
    }),
  ).toBeVisible()
}
test('pilot workflow records only coupon uses, counts unique cards and preserves cancellations', async ({
  page,
  context,
  baseURL,
}, info) => {
  const headers = await login(page, context, baseURL!)
  await page
    .getByLabel('Поиск по имени или телефону', { exact: true })
    .fill('Клиент пилота')
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page
    .getByRole('button', { name: 'Добавить клиента', exact: true })
    .click()
  await page.getByLabel('Имя / ФИО', { exact: true }).fill('Клиент пилота')
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(
    page.getByText('Карточка сохранена.', { exact: true }),
  ).toBeVisible()
  await statistics(page, [0, 0, 0, 0, 0, 0], /^—$/)
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await page
    .getByLabel('Только первое посещение специалиста', { exact: true })
    .check()
  await page.getByLabel('Общий лимит', { exact: true }).selectOption('false')
  await page.getByLabel('Всего использований', { exact: true }).fill('1')
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await expect(page.getByText('Купон выдан.', { exact: true })).toBeVisible()
  const firstCoupon = (
    await (
      await context.request.post('/api/coupons/search', { headers, data: {} })
    ).json()
  ).items[0]
  const publicPage = await context.newPage()
  await publicPage.goto(
    (await page
      .getByRole('link', { name: 'Открыть карточку для клиента' })
      .getAttribute('href'))!,
  )
  await expect(
    publicPage.getByRole('heading', { name: `Купон № ${firstCoupon.code}` }),
  ).toBeVisible()
  await publicPage.close()
  let form = await chooseClient(page)
  await expect(form.getByLabel(single, { exact: true })).not.toBeChecked()
  await expect(form.getByLabel(first, { exact: true })).not.toBeChecked()
  await form.getByLabel(single, { exact: true }).check()
  await form.getByLabel(first, { exact: true }).check()
  await form
    .getByRole('button', { name: 'Подтвердить использование', exact: true })
    .click()
  await expect(
    form.getByText('Использование подтверждено.', { exact: true }),
  ).toBeVisible()
  await statistics(page, [1, 1, 1, 1, 1, 0], /^100\s*%$/)
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page
    .getByRole('button', { name: '← Назад к купонам', exact: true })
    .click()
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await page.getByLabel('Вид скидки', { exact: true }).selectOption('fixed')
  await page.getByLabel('Скидка, ₽', { exact: true }).fill('350,25')
  await page
    .getByLabel('На одного клиента', { exact: true })
    .selectOption('true')
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await expect(page.getByText('Купон выдан.', { exact: true })).toBeVisible()
  const secondCoupon = (
    await (
      await context.request.post('/api/coupons/search', { headers, data: {} })
    ).json()
  ).items[0]
  form = await chooseClient(page)
  for (let i = 0; i < 2; i++) {
    await expect(form.getByLabel(single, { exact: true })).not.toBeChecked()
    await form.getByLabel(single, { exact: true }).check()
    await form
      .getByRole('button', { name: 'Подтвердить использование', exact: true })
      .click()
    await expect(
      form.getByText('Использование подтверждено.', { exact: true }),
    ).toBeVisible()
  }
  await page.getByRole('button', { name: 'История', exact: true }).click()
  await page.locator('.coupon-revoke > summary').click()
  await page
    .getByLabel('Причина отзыва', { exact: true })
    .fill('Завершена тестовая выдача')
  await page
    .getByLabel('Понимаю, что отзыв окончательный', { exact: true })
    .check()
  await page
    .getByRole('button', { name: 'Отозвать купон', exact: true })
    .click()
  await expect(
    page.getByText('Купон окончательно отозван.', { exact: true }),
  ).toBeVisible()
  const unused = await context.request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { discount_type: 'percent', value: '5' },
  })
  expect(unused.status()).toBe(201)
  await statistics(page, [3, 2, 3, 1, 1, 0], /^66,7\s*%$/)
  await expect(
    page.getByText(
      'За все время. Только ваши купоны и записанные применения.',
      { exact: true },
    ),
  ).toBeVisible()
  await page.screenshot({
    path: info.outputPath('pilot-statistics.png'),
    fullPage: true,
  })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await page
    .getByRole('button', {
      name: 'Посмотреть купоны с применениями',
      exact: true,
    })
    .click()
  await expect(
    page.getByLabel('Применения купона', { exact: true }),
  ).toHaveValue('used')
  await expect(page.locator('.client-rows > li')).toHaveCount(2)
  await page
    .getByRole('button', { name: new RegExp(`№ ${firstCoupon.code}.*Открыть`) })
    .click()
  await cancelFirst(page, 'Отмена тестового первого посещения')
  await statistics(page, [3, 1, 2, 1, 0, 1], /^33,3\s*%$/)
  await page
    .getByRole('button', {
      name: 'Посмотреть купоны с применениями',
      exact: true,
    })
    .click()
  await expect(page.locator('.client-rows > li')).toHaveCount(1)
  await page
    .getByRole('button', {
      name: new RegExp(`№ ${secondCoupon.code}.*Открыть`),
    })
    .click()
  await cancelFirst(page, 'Первая тестовая отмена многоразового')
  await statistics(page, [3, 1, 1, 1, 0, 2], /^33,3\s*%$/)
  await page
    .getByRole('button', {
      name: 'Посмотреть купоны с применениями',
      exact: true,
    })
    .click()
  await page
    .getByRole('button', {
      name: new RegExp(`№ ${secondCoupon.code}.*Открыть`),
    })
    .click()
  await cancelFirst(page, 'Последняя тестовая отмена многоразового')
  await statistics(page, [3, 0, 0, 0, 0, 3], /^0\s*%$/)
  await page
    .getByRole('button', { name: 'Посмотреть все купоны', exact: true })
    .click()
  await expect(
    page.getByLabel('Применения купона', { exact: true }),
  ).toHaveValue('all')
  await page.locator('.coupon-filters > summary').click()
  await page
    .getByLabel('Применения купона', { exact: true })
    .selectOption('unused')
  await expect(page.locator('.client-rows > li')).toHaveCount(3)
  await page.getByRole('button', { name: 'Клиенты', exact: true }).click()
  await page.getByRole('button', { name: /Клиент пилота.*Открыть/ }).click()
  const history = page.getByRole('region', {
    name: 'Использования купонов клиента',
    exact: true,
  })
  await expect(history.getByText('Отменено', { exact: true })).toHaveCount(3)
})

test('empty and failed statistics preserve owner isolation and require a live session', async ({
  page,
  context,
  request,
  baseURL,
}) => {
  await login(page, context, baseURL!)
  const foreignLogin = `other-pilot-${randomBytes(8).toString('hex')}`,
    foreignPassword = randomBytes(12).toString('base64url')
  manage(
    [
      'create-user',
      '--login',
      foreignLogin,
      '--name',
      'Другой специалист',
      '--password-stdin',
    ],
    foreignPassword,
  )
  const foreign = await (
    await request.post('/api/auth/login', {
      headers: { Origin: new URL(baseURL!).origin },
      data: { login: foreignLogin, password: foreignPassword },
    })
  ).json()
  const headers = {
    Origin: new URL(baseURL!).origin,
    'X-CSRF-Token': foreign.csrf_token,
  }
  expect(
    (
      await request.post('/api/coupons', {
        headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
        data: { discount_type: 'percent', value: '10' },
      })
    ).status(),
  ).toBe(201)
  expect(
    (await (await request.get('/api/coupons/statistics')).json())
      .issued_coupons,
  ).toBe(1)
  await statistics(page, [0, 0, 0, 0, 0, 0], /^—$/)
  expect(
    (
      await context.request.get(
        `/api/coupons/statistics?specialist_id=${foreign.user.id}`,
      )
    ).status(),
  ).toBe(400)
  await page.route('**/api/coupons/statistics', (route) =>
    route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: '{"status":"unavailable"}',
    }),
  )
  await page
    .getByRole('button', { name: 'Обновить статистику', exact: true })
    .click()
  await expect(page.getByRole('alert')).toHaveText(
    'Не удалось обновить статистику. Попробуйте еще раз.',
  )
  await expect(page.getByTestId('stat-issued')).toHaveCount(0)
  await page.unroute('**/api/coupons/statistics')
  await page.route('**/api/coupons/statistics', (route) =>
    route.abort('failed'),
  )
  await page
    .getByRole('button', { name: 'Обновить статистику', exact: true })
    .click()
  await expect(page.getByRole('alert')).toHaveText(
    'Нет связи с сервисом. Повторите попытку.',
  )
  await page.unroute('**/api/coupons/statistics')
  await page
    .getByRole('button', { name: 'Обновить статистику', exact: true })
    .click()
  await expect(page.getByTestId('stat-issued')).toHaveText('0')
  await page.setViewportSize({ width: 320, height: 800 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth))
    .toBeLessThanOrEqual(320)
  await page.route('**/api/coupons/statistics', (route) =>
    route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: '{"status":"unauthenticated"}',
    }),
  )
  await page
    .getByRole('button', { name: 'Обновить статистику', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Вход в кабинет' }),
  ).toBeVisible()
  await expect(page.getByTestId('stat-issued')).toHaveCount(0)
})
