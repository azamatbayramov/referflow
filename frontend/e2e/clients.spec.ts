import { randomBytes } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { manage } from './helpers'
async function login(page: Page) {
  const user = `clients-${randomBytes(8).toString('hex')}`
  const password = randomBytes(12).toString('base64url')
  manage(
    [
      'create-user',
      '--login',
      user,
      '--name',
      'Тестовый специалист',
      '--password-stdin',
    ],
    password,
  )
  await page.goto('/')
  await page.getByLabel('Логин', { exact: true }).fill(user)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Карточки клиентов' }),
  ).toBeVisible()
}
test('find, create, correct contacts, duplicate warning, archive and restore', async ({
  page,
  context,
  baseURL,
}) => {
  await login(page)
  await page.getByRole('button', { name: 'Добавить клиента' }).click()
  await page.getByLabel('Имя / ФИО').fill('Анна Тестовая')
  await page.getByLabel('Телефон (необязательно)').fill('+7 (900) 000-00-01')
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(
    page.getByRole('status').filter({ hasText: 'Карточка сохранена.' }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  const headers = {
    Origin: new URL(baseURL!).origin,
    'X-CSRF-Token': session.csrf_token,
  }
  const before = await (
    await context.request.post('/api/clients/search', {
      headers,
      data: { query: 'Анна' },
    })
  ).json()
  expect(before.items).toHaveLength(1)
  const id = before.items[0].id
  await page.getByLabel('Имя / ФИО').fill('Анна Исправленная')
  await page.getByLabel('Телефон (необязательно)').fill('+7 900 000 00 02')
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(page.getByLabel('Имя / ФИО')).toHaveValue('Анна Исправленная')
  await page.getByRole('button', { name: 'Закрыть', exact: true }).click()
  await page.getByLabel('Поиск по имени или телефону').fill('9000000002')
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await page.getByRole('button', { name: /Анна Исправленная.*Открыть/ }).click()
  const after = await (await context.request.get(`/api/clients/${id}`)).json()
  expect(after.client.name).toBe('Анна Исправленная')
  expect(after.events.map((e: { action: string }) => e.action)).toEqual([
    'updated',
    'created',
  ])
  await page.getByRole('button', { name: 'В архив', exact: true }).click()
  await expect(
    page.getByText('Статус карточки изменен.', { exact: true }),
  ).toBeVisible()
  await page.getByLabel('Показать', { exact: true }).selectOption('archived')
  await page
    .getByRole('button', { name: /Анна Исправленная.*В архиве/ })
    .click()
  await page
    .getByRole('button', { name: 'Вернуть из архива', exact: true })
    .click()
  await page.getByLabel('Показать', { exact: true }).selectOption('active')
  await page.getByRole('button', { name: 'Добавить клиента' }).click()
  await page.getByLabel('Имя / ФИО').fill('Другой Тестовый')
  await page.getByLabel('Телефон (необязательно)').fill('+7 (900) 000-00-02')
  await expect(
    page.getByRole('heading', { name: 'Похожие карточки' }),
  ).toBeVisible()
  await page
    .getByRole('checkbox', { name: 'Это отдельный клиент, сохранить карточку' })
    .check()
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(
    page.getByText('Карточка сохранена.', { exact: true }),
  ).toBeVisible()
  const shared = await (
    await context.request.post('/api/clients/search', {
      headers,
      data: { query: '9000000002' },
    })
  ).json()
  expect(shared.items).toHaveLength(2)
  expect(shared.items.some((c: { id: string }) => c.id === id)).toBe(true)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})
test('retry after lost create response keeps one card without a phone', async ({
  page,
  context,
  baseURL,
}) => {
  await login(page)
  await page.getByRole('button', { name: 'Добавить клиента' }).click()
  await page.getByLabel('Имя / ФИО').fill('Без Телефона')
  await page.getByLabel('Телефон (необязательно)').fill('123')
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(page.getByRole('alert')).toContainText('Проверьте имя и телефон')
  await expect(page.getByLabel('Имя / ФИО')).toBeEnabled()
  await page.getByLabel('Телефон (необязательно)').fill('')
  let first = true
  await page.route('**/api/clients', async (route) => {
    if (first) {
      first = false
      await route.fetch()
      await route.abort('failed')
    } else await route.continue()
  })
  await page
    .getByRole('button', { name: 'Сохранить карточку', exact: true })
    .click()
  await expect(page.getByLabel('Имя / ФИО')).toBeDisabled()
  await page
    .getByRole('button', { name: 'Повторить сохранение', exact: true })
    .click()
  await expect(
    page.getByText('Карточка сохранена.', { exact: true }),
  ).toBeVisible()
  const session = await (await context.request.get('/api/auth/session')).json()
  const response = await context.request.post('/api/clients/search', {
    headers: {
      Origin: new URL(baseURL!).origin,
      'X-CSRF-Token': session.csrf_token,
    },
    data: { query: 'Без Телефона' },
  })
  const result = await response.json()
  expect(result.items).toHaveLength(1)
  expect(result.items[0].phone).toBe('')
})
