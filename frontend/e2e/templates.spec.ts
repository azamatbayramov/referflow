import { randomBytes } from 'node:crypto'
import { expect, test, type Page, type BrowserContext } from '@playwright/test'
import { manage } from './helpers'
async function setup(page: Page, context: BrowserContext, baseURL: string) {
  const login = `templates-${randomBytes(8).toString('hex')}`
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
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Шаблоны купонов', exact: true }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  return { Origin: new URL(baseURL).origin, 'X-CSRF-Token': session.csrf_token }
}
const saved = 'Шаблон сохранен. Условия выданных купонов не изменились.'
test('immutable templates retain coupon sources and survive archiving', async ({
  page,
  context,
  baseURL,
}, info) => {
  const headers = await setup(page, context, baseURL!)
  await page
    .getByRole('button', { name: 'Создать шаблон', exact: true })
    .click()
  await page
    .getByLabel('Название шаблона', { exact: true })
    .fill('Близким Тестовый')
  await page.getByLabel('Скидка, %', { exact: true }).fill('12,50')
  await page.getByLabel('Общий лимит', { exact: true }).selectOption('false')
  await page.getByLabel('Всего использований', { exact: true }).fill('2')
  await page
    .getByLabel('Только первое посещение специалиста', { exact: true })
    .check()
  await page.getByLabel('Указать срок действия', { exact: true }).check()
  await page
    .getByLabel('Последний день действия', { exact: true })
    .fill('2099-12-31')
  await page
    .getByLabel('Ограничения по услугам (необязательно)', { exact: true })
    .fill('Условия полностью\nВторая строка')
  await page
    .getByRole('button', { name: 'Сохранить шаблон', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  await page.screenshot({
    path: info.outputPath('templates.png'),
    fullPage: true,
  })
  for (const value of ['15', '12.50']) {
    await page
      .getByRole('button', { name: 'Выдать по шаблону', exact: true })
      .click()
    await expect(page.getByLabel('Скидка, %', { exact: true })).toHaveValue(
      '12.50',
    )
    await expect(
      page.getByLabel('Всего использований', { exact: true }),
    ).toHaveValue('2')
    await expect(
      page.getByLabel('Использований на клиента', { exact: true }),
    ).toHaveValue('1')
    await expect(
      page.getByLabel('Только первое посещение специалиста', { exact: true }),
    ).toBeChecked()
    await expect(
      page.getByLabel('Последний день действия', { exact: true }),
    ).toHaveValue('2099-12-31')
    await expect(
      page.getByLabel('Ограничения по услугам (необязательно)', {
        exact: true,
      }),
    ).toHaveValue('Условия полностью\nВторая строка')
    await expect(
      page.getByLabel('Клиент, которому выдан купон', { exact: true }),
    ).toHaveValue('Не указан')
    await page.getByLabel('Скидка, %', { exact: true }).fill(value)
    await page
      .getByRole('button', { name: 'Выдать купон', exact: true })
      .click()
    await expect(page.getByText('Купон выдан.', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
    await expect(
      page.getByRole('button', { name: 'Выдать по шаблону', exact: true }),
    ).toBeVisible()
  }
  const issued = await (
    await context.request.post('/api/coupons/search', { headers, data: {} })
  ).json()
  expect(issued.items).toHaveLength(2)
  expect(new Set(issued.items.map((c: { code: string }) => c.code)).size).toBe(
    2,
  )
  expect(issued.items.map((c: { value: string }) => c.value).sort()).toEqual([
    '12.50',
    '15.00',
  ])
  for (const c of issued.items) {
    expect(c.used_count).toBe(0)
    expect(c.referrer_client_id).toBeNull()
    expect(c.template_id).toBeTruthy()
  }
  await expect(
    page
      .getByRole('region', { name: 'Шаблоны купонов', exact: true })
      .getByText('Условия полностью\nВторая строка', { exact: true }),
  ).toBeVisible()
  await page
    .getByRole('button', { name: 'Действия: Близким Тестовый', exact: true })
    .click()
  await page
    .getByRole('button', { name: 'Созданные купоны', exact: true })
    .click()
  await expect(page.getByText('Купоны из выбранного шаблона')).toBeVisible()
  await expect(page.locator('.coupon-list > li')).toHaveCount(2)
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await page
    .getByRole('button', { name: 'Действия: Близким Тестовый', exact: true })
    .click()
  await page.getByRole('button', { name: 'В архив', exact: true }).click()
  await expect(
    page.getByLabel('Название шаблона', { exact: true }),
  ).toHaveCount(0)
  await page
    .getByRole('button', { name: 'Отправить в архив', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Шаблоны не найдены', exact: true }),
  ).toBeVisible()
  await page.getByLabel('Показывать архивированные', { exact: true }).check()
  await expect(
    page.getByRole('heading', { name: 'Близким Тестовый', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Выдать по шаблону', exact: true }),
  ).toHaveCount(0)
  for (const c of issued.items) {
    const detail = await (
      await context.request.get(`/api/coupons/${c.id}`)
    ).json()
    expect(detail.coupon).toEqual(c)
    const publicCard = await (
      await context.request.get(`/api/public/coupons/${c.id}`)
    ).json()
    expect(publicCard.value).toBe(c.value)
    expect(publicCard).not.toHaveProperty('name')
    expect(publicCard).not.toHaveProperty('template_id')
    expect(JSON.stringify(publicCard)).not.toContain('Близким Тестовый')
  }
  await page
    .getByRole('button', { name: 'Действия: Близким Тестовый', exact: true })
    .click()
  await page.getByRole('button', { name: 'Восстановить', exact: true }).click()
  await page
    .getByRole('button', { name: 'Восстановить шаблон', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  await page.getByLabel('Показывать архивированные', { exact: true }).uncheck()
  await page.getByLabel('Название шаблона', { exact: true }).fill('НЕ НАЙДЕНО')
  await expect(
    page.getByRole('heading', { name: 'Шаблоны не найдены', exact: true }),
  ).toBeVisible()
  await page.getByLabel('Название шаблона', { exact: true }).fill('близким')
  await page
    .getByRole('button', { name: 'Выдать по шаблону', exact: true })
    .click()
  await expect(page.getByLabel('Скидка, %', { exact: true })).toHaveValue(
    '12.50',
  )
  await expect(
    page.getByLabel('На одного клиента', { exact: true }),
  ).toHaveValue('false')
  await expect(
    page.getByLabel('Указать срок действия', { exact: true }),
  ).toBeChecked()
  await expect(
    page.getByRole('button', {
      name: 'Близким Тестовый · 12,50 %',
      exact: true,
    }),
  ).toBeVisible()
  await page.setViewportSize({ width: 320, height: 800 })
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(320)
})

test('lost template creation and archive responses preserve one key across sections', async ({
  page,
  context,
  baseURL,
}) => {
  const headers = await setup(page, context, baseURL!)
  await page
    .getByRole('button', { name: 'Создать шаблон', exact: true })
    .click()
  await page
    .getByLabel('Название шаблона', { exact: true })
    .fill('Повтор Тестовый')
  let lose = true
  const createKeys: string[] = [],
    updateKeys: string[] = []
  await page.route('**/api/coupons/templates', async (route) => {
    createKeys.push(route.request().headers()['idempotency-key'])
    if (lose) {
      lose = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page
    .getByRole('button', { name: 'Сохранить шаблон', exact: true })
    .click()
  await expect(
    page.getByRole('button', {
      name: 'Повторить сохранение шаблона',
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    page.getByLabel('Название шаблона', { exact: true }),
  ).toBeDisabled()
  await expect(
    page.getByRole('button', { name: '← Назад к шаблонам', exact: true }),
  ).toBeDisabled()
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await page
    .getByRole('button', { name: 'Повторить сохранение шаблона', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  expect(createKeys).toHaveLength(2)
  expect(createKeys[0]).toBe(createKeys[1])
  const all = await (
    await context.request.post('/api/coupons/templates/search', {
      headers,
      data: {},
    })
  ).json()
  expect(all.items).toHaveLength(1)
  const id = all.items[0].id
  lose = true
  await page.route(`**/api/coupons/templates/${id}`, async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.continue()
      return
    }
    updateKeys.push(route.request().headers()['idempotency-key'])
    if (lose) {
      lose = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page
    .getByRole('button', { name: 'Отправить в архив', exact: true })
    .click()
  await expect(
    page.getByRole('button', {
      name: 'Повторить сохранение шаблона',
      exact: true,
    }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Статистика', exact: true }).click()
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await page
    .getByRole('button', { name: 'Повторить сохранение шаблона', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  expect(updateKeys).toHaveLength(2)
  expect(updateKeys[0]).toBe(updateKeys[1])
  expect(updateKeys[0]).not.toBe(createKeys[0])
  const updated = await (
    await context.request.get(`/api/coupons/templates/${id}`)
  ).json()
  expect(updated.version).toBe(2)
  expect(updated.value).toBe('10.00')
  expect(updated.archived).toBe(true)
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  await page.getByLabel('Показывать архивированные', { exact: true }).check()
  await page
    .getByRole('button', { name: 'Действия: Повтор Тестовый', exact: true })
    .click()
  await page.getByRole('button', { name: 'Восстановить', exact: true }).click()
  await page
    .getByRole('button', { name: 'Восстановить шаблон', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  await page
    .getByRole('button', { name: 'Выдать по шаблону', exact: true })
    .click()
  let loseCoupon = true
  const couponKeys: string[] = []
  await page.route('**/api/coupons', async (route) => {
    couponKeys.push(route.request().headers()['idempotency-key'])
    if (loseCoupon) {
      loseCoupon = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page.getByRole('button', { name: 'Выдать купон', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Повторить создание', exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Шаблоны', exact: true }).click()
  await page
    .getByRole('button', { name: 'Выдать по шаблону', exact: true })
    .click()
  await expect(page.getByRole('alert')).toHaveText(
    'Сначала завершите отправку в открытом купоне.',
  )
  await page.getByRole('button', { name: 'Купоны', exact: true }).click()
  await page
    .getByRole('button', { name: 'Повторить создание', exact: true })
    .click()
  await expect(page.getByText('Купон выдан.', { exact: true })).toBeVisible()
  expect(couponKeys).toHaveLength(2)
  expect(couponKeys[0]).toBe(couponKeys[1])
  const coupons = await (
    await context.request.post('/api/coupons/search', { headers, data: {} })
  ).json()
  expect(coupons.items).toHaveLength(1)
})

test('stale archive actions require refresh and issuance rechecks archived templates', async ({
  page,
  context,
  baseURL,
}) => {
  const headers = await setup(page, context, baseURL!)
  const response = await context.request.post('/api/coupons/templates', {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {
      name: 'Конкуренция Тестовый',
      discount_type: 'fixed',
      value: '100',
    },
  })
  expect(response.status()).toBe(201)
  const t = await response.json()
  await page.getByLabel('Название шаблона', { exact: true }).fill('Конкуренция')
  await page
    .getByRole('button', {
      name: 'Действия: Конкуренция Тестовый',
      exact: true,
    })
    .click()
  await page.getByRole('button', { name: 'В архив', exact: true }).click()
  const edit = {
    name: t.name,
    discount_type: t.discount_type,
    value: '100',
    restrictions: '',
    total_limit: null,
    per_client_limit: 1,
    first_visit_only: false,
    expires_on: null,
    archived: true,
    version: 1,
  }
  expect(
    (
      await context.request.put(`/api/coupons/templates/${t.id}`, {
        headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
        data: edit,
      })
    ).status(),
  ).toBe(200)
  await page
    .getByRole('button', { name: 'Отправить в архив', exact: true })
    .click()
  await expect(
    page.getByRole('button', {
      name: 'Загрузить актуальную версию',
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Отправить в архив', exact: true }),
  ).toBeDisabled()
  await page
    .getByRole('button', { name: 'Загрузить актуальную версию', exact: true })
    .click()
  await page
    .getByRole('button', { name: 'Восстановить шаблон', exact: true })
    .click()
  await expect(page.getByText(saved, { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: '← Назад к шаблонам', exact: true })
    .click()
  expect(
    (
      await context.request.put(`/api/coupons/templates/${t.id}`, {
        headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
        data: { ...edit, version: 3, archived: true },
      })
    ).status(),
  ).toBe(200)
  await page
    .getByRole('button', { name: 'Выдать по шаблону', exact: true })
    .click()
  await expect(page.getByRole('alert')).toHaveText(
    'Шаблон в архиве. Восстановите его перед выдачей.',
  )
  const issued = await (
    await context.request.post('/api/coupons/search', { headers, data: {} })
  ).json()
  expect(issued.items).toHaveLength(0)
})
