import { expect, test } from '@playwright/test'

test('serves the app and API through the same origin', async ({
  page,
  request,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  const response = await page.goto('/')
  expect(response?.status()).toBe(200)
  await expect(page).toHaveTitle('Referflow')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  expect(errors).toEqual([])

  const health = await request.get('/api/healthz')
  expect(health.status()).toBe(200)
  expect(await health.json()).toEqual({ status: 'ok' })
  const ready = await request.get('/api/readyz')
  expect(ready.status()).toBe(200)
  expect(await ready.json()).toEqual({ status: 'ready' })

  // Caddy must not turn an unknown API route into a successful SPA response.
  const missing = await request.get('/api/missing')
  expect(missing.status()).toBe(404)
  expect(await missing.json()).toEqual({ status: 'not_found' })
  const root = await request.get('/api')
  expect(root.status()).toBe(404)

  // Deep links must still load the client app once client-side routing is added.
  const deepLink = await request.get('/future-route')
  expect(deepLink.status()).toBe(200)
  expect(deepLink.headers()['content-type']).toContain('text/html')
  const asset = await request.get('/assets/missing.js')
  expect(asset.status()).toBe(404)
})
