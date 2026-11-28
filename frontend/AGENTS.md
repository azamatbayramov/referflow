# Frontend

- Vue 3 SFCs and strict TypeScript, built with Vite. Use npm and commit `package-lock.json`; install with `npm ci`.
- Keep UI accessible, responsive, and in Russian unless the task says otherwise. Do not expose infrastructure details in product flows.
- API requests must use the same-origin `/api/` prefix. Do not embed environment hostnames or secrets in the browser bundle.
- Vite proxies `/api` to the host API on port 8081. Caddy proxies it to `api:8080` without stripping the prefix.
- Add a router, state store, UI kit, and API client only when a real feature needs them; explain the choice.
- `Caddyfile` belongs to this image. Keep `/api` errors out of the SPA fallback and missing static assets as 404 responses.
- Checks: `npm run format:check`, `npm run lint`, `npm run typecheck`, `npm run build`.
- Runtime changes: start the Compose stack, install Chromium with `npx playwright install chromium`, then `npm run test:e2e`. Browser checks target `BASE_URL` or `http://127.0.0.1:8080`.

- Coupon workspace is kept mounted across cabinet sections to preserve draft, selection and idempotency keys. Do not unmount use/history forms when changing coupon tabs: mutation responses must still update the coupon. Logout/session expiry must still destroy private state. See `../docs/interface.md`.
