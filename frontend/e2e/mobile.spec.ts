import { randomBytes } from 'node:crypto'
import { expect, test, type Page, type BrowserContext } from '@playwright/test'
import { manage } from './helpers'
async function setup(page: Page, context: BrowserContext, baseURL: string) {
  const login = `phone-${randomBytes(8).toString('hex')}`
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
  await expect(
    page.getByRole('heading', { name: 'Купоны', exact: true }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  return { Origin: new URL(baseURL).origin, 'X-CSRF-Token': session.csrf_token }
}
async function shareCard(page: Page, context: BrowserContext, baseURL: string) {
  const headers = await setup(page, context, baseURL)
  const response = await context.request.post('/api/coupons', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { discount_type: 'percent', value: '15' },
  })
  expect(response.status()).toBe(201)
  const c = await response.json()
  await page.getByLabel('Номер купона', { exact: true }).fill(c.code)
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page
    .getByRole('button', { name: new RegExp(`№ ${c.code}.*Открыть`) })
    .click()
  await page.getByRole('button', { name: 'Отправить', exact: true }).click()
  return {
    headers,
    coupon: c,
    card: page.getByRole('region', { name: 'Передать купон', exact: true }),
  }
}

test('phone navigation stays reachable, forms fit and keyboard does not compete with the dock', async ({
  page,
  context,
  baseURL,
}, info) => {
  await page.setViewportSize({ width: 320, height: 640 })
  await setup(page, context, baseURL!)
  const nav = page.getByRole('navigation', { name: 'Разделы кабинета' })
  for (const viewport of [
    { width: 320, height: 640 },
    { width: 430, height: 850 },
    { width: 844, height: 390 },
  ]) {
    await page.setViewportSize(viewport)
    const box = (await nav.boundingBox())!
    expect(box.y).toBeGreaterThan(viewport.height - 120)
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height + 1)
    for (const name of ['Купоны', 'Шаблоны', 'Клиенты', 'Статистика']) {
      const button = nav.getByRole('button', { name, exact: true })
      const target = (await button.boundingBox())!
      expect(target.height).toBeGreaterThanOrEqual(48)
      expect(target.width).toBeGreaterThanOrEqual(44)
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true)
  }
  await page.setViewportSize({ width: 320, height: 640 })
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  const value = page.getByLabel('Скидка, %', { exact: true })
  expect(await value.evaluate((el) => getComputedStyle(el).fontSize)).toBe(
    '16px',
  )
  await value.fill('17.25')
  // Emulate only the browser viewport resize; no OS share/keyboard UI is automated.
  await value.focus()
  await page.evaluate(() => {
    const viewport = window.visualViewport!
    const height = viewport.height
    Object.defineProperty(viewport, 'height', {
      configurable: true,
      get: () => height - 240,
    })
    viewport.dispatchEvent(new Event('resize'))
  })
  await expect(nav).toBeHidden()
  await page.evaluate(() => {
    Object.defineProperty(window.visualViewport, 'height', {
      configurable: true,
      get: () => window.innerHeight,
    })
    window.visualViewport!.dispatchEvent(new Event('resize'))
  })
  await expect(nav).toBeVisible()
  await nav.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Шаблоны купонов', exact: true }),
  ).toBeVisible()
  await nav.getByRole('button', { name: 'Купоны', exact: true }).click()
  await expect(value).toHaveValue('17.25')
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await expect(page.getByText('Купон выдан.', { exact: true })).toBeVisible()
  const image = page.getByRole('button', {
    name: 'Сохранить изображение',
    exact: true,
  })
  await image.scrollIntoViewIfNeeded()
  const target = (await image.boundingBox())!,
    dock = (await nav.boundingBox())!
  expect(target.y + target.height).toBeLessThanOrEqual(dock.y)
  await page.screenshot({
    path: info.outputPath('phone-share.png'),
    fullPage: true,
  })
  await nav.getByRole('button', { name: 'Клиенты', exact: true }).click()
  await page
    .getByRole('button', { name: 'Добавить клиента', exact: true })
    .click()
  await expect(page.locator('.clients-list')).toBeHidden()
  await expect(
    page.getByRole('heading', { name: 'Новый клиент', exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Закрыть', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Карточки клиентов', exact: true }),
  ).toBeVisible()
})

test('client creation retains its draft and replay key while moving through phone sections', async ({
  page,
  context,
  baseURL,
}) => {
  await page.setViewportSize({ width: 320, height: 640 })
  const headers = await setup(page, context, baseURL!)
  await page.getByRole('button', { name: 'Клиенты', exact: true }).click()
  await page
    .getByRole('button', { name: 'Добавить клиента', exact: true })
    .click()
  await page.getByLabel('Имя / ФИО', { exact: true }).fill('Клиент Телефонный')
  let lose = true
  const keys: string[] = []
  await page.route('**/api/clients', async (route) => {
    keys.push(route.request().headers()['idempotency-key'])
    if (lose) {
      lose = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(
    page.getByRole('button', { name: 'Повторить сохранение', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Закрыть', exact: true }),
  ).toBeDisabled()
  for (const name of ['Купоны', 'Шаблоны', 'Статистика', 'Клиенты']) {
    await page
      .getByRole('navigation')
      .getByRole('button', { name, exact: true })
      .click()
  }
  await expect(page.getByLabel('Имя / ФИО', { exact: true })).toHaveValue(
    'Клиент Телефонный',
  )
  await expect(page.getByLabel('Имя / ФИО', { exact: true })).toBeDisabled()
  await page
    .getByRole('button', { name: 'Повторить сохранение', exact: true })
    .click()
  await expect(
    page.getByText('Карточка сохранена.', { exact: true }),
  ).toBeVisible()
  expect(keys).toHaveLength(2)
  expect(keys[0]).toBe(keys[1])
  const found = await (
    await context.request.post('/api/clients/search', {
      headers,
      data: { query: 'Клиент Телефонный' },
    })
  ).json()
  expect(found.items).toHaveLength(1)
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page.getByText('Профиль', { exact: true }).click()
  await page.getByRole('button', { name: 'Выйти', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Вход в кабинет', exact: true }),
  ).toBeVisible()
  await expect(page.locator('#client-name')).toHaveCount(0)
})

test('native sharing passes only a public link and handles cancellation and failure', async ({
  page,
  context,
  baseURL,
}) => {
  await page.addInitScript(() => {
    const state = { outcome: 'success', calls: [] as ShareData[] }
    Object.assign(window, { testShare: state })
    Object.defineProperty(navigator, 'share', {
      configurable: true,
      value: async (data: ShareData) => {
        state.calls.push(data)
        if (state.outcome === 'cancel')
          throw new DOMException('Dismissed', 'AbortError')
        if (state.outcome === 'error') throw new Error('Unavailable')
      },
    })
  })
  const { coupon, card } = await shareCard(page, context, baseURL!)
  const send = card.getByRole('button', {
    name: 'Отправить купон',
    exact: true,
  })
  await send.click()
  await expect(card.getByRole('status')).toHaveText(
    'Ссылка передана в выбранное приложение.',
  )
  expect(
    await page.evaluate(
      () =>
        (window as unknown as { testShare: { calls: ShareData[] } }).testShare
          .calls,
    ),
  ).toEqual([
    {
      title: 'Купон Referflow',
      url: new URL(`/c/${coupon.id}`, baseURL!).href,
    },
  ])
  await page.evaluate(() => {
    ;(
      window as unknown as { testShare: { outcome: string } }
    ).testShare.outcome = 'cancel'
  })
  await send.click()
  await expect(card.getByRole('status')).toHaveCount(0)
  await expect(card.getByRole('alert')).toHaveCount(0)
  await page.evaluate(() => {
    ;(
      window as unknown as { testShare: { outcome: string } }
    ).testShare.outcome = 'error'
  })
  await send.click()
  await expect(card.getByRole('alert')).toContainText('Скопируйте ссылку')
  await expect(card.getByLabel('Ссылка на купон', { exact: true })).toHaveValue(
    new URL(`/c/${coupon.id}`, baseURL!).href,
  )
  const detail = await (
    await context.request.get(`/api/coupons/${coupon.id}`)
  ).json()
  expect(detail.coupon.used_count).toBe(0)
  expect(detail.events).toHaveLength(1)
})

test('unsupported sharing and denied clipboard keep a manually copyable public link', async ({
  page,
  context,
  baseURL,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'share', {
      configurable: true,
      value: undefined,
    })
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error('Denied')
        },
      },
    })
  })
  const { coupon, card } = await shareCard(page, context, baseURL!)
  await expect(
    card.getByRole('button', { name: 'Отправить купон', exact: true }),
  ).toHaveCount(0)
  await card
    .getByRole('button', { name: 'Скопировать ссылку', exact: true })
    .click()
  const link = card.getByLabel('Ссылка на купон', { exact: true })
  await expect(link).toHaveValue(new URL(`/c/${coupon.id}`, baseURL!).href)
  expect(
    await link.evaluate((el) => {
      const input = el as HTMLInputElement
      return (
        input.selectionStart === 0 && input.selectionEnd === input.value.length
      )
    }),
  ).toBe(true)
})
