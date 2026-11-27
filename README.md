# Referflow

Монорепозиторий сервиса рекомендаций. Первый этап — запускаемый каркас без бизнес-логики: Go API, Vue-приложение, PostgreSQL, контейнеры, CI и инструкции для разработки с агентами.

## Быстрый запуск

Нужны Docker Engine / Docker Desktop с Compose v2.24+ и Make.

```bash
git clone https://github.com/azamatbayramov/referflow.git
cd referflow
make up
make smoke
```

`make up` создает локальный `.env`, собирает образы и ждет готовности сервисов.

| Адрес | Назначение |
| --- | --- |
| http://localhost:8080 | Приложение и единая точка входа в API |
| http://localhost:8080/api/healthz | Жив ли процесс API |
| http://localhost:8080/api/readyz | Доступен ли PostgreSQL |
| http://localhost:8081 | Прямой доступ к API при разработке |
| `127.0.0.1:5432` | Локальный PostgreSQL |

```bash
make logs
make down
```

`make down` сохраняет данные. Локальные порты привязаны к loopback; пароли из `.env.example` предназначены только для разработки.

## Структура

| Путь | Содержимое |
| --- | --- |
| `backend/` | Go 1.27, `net/http`, `slog`, pgx, graceful shutdown и health endpoints |
| `frontend/` | Vue 3, TypeScript, Vite, ESLint, Prettier и Playwright |
| `api/openapi.yaml` | Контракт инфраструктурных HTTP endpoints |
| `compose.yaml` | Локальный стек с образами из исходников |
| `deploy/` | Отдельный production Compose для GHCR-образов и HTTPS |
| `scripts/` | Smoke-проверки, проверка отказа БД и запуск развертывания |
| `.github/` | CI, публикация образов, Dependabot и шаблоны задач/PR |
| `AGENTS.md` | Общие правила и карта проекта для агентов |
| `docs/` | Архитектура, разработка, эксплуатация и процесс работы агентов |

## Разработка

Для работы на хосте нужны Go 1.27.4, Node.js из `.node-version`, npm, Make и Docker для PostgreSQL.

```bash
make setup
make db
make dev-api
```

В другом терминале:

```bash
make dev-web
```

Фронтенд с hot reload доступен на http://localhost:5173. Если полный стек уже запущен через `make up`, можно использовать его API и запустить только `make dev-web`.

```bash
make check
cd frontend
npx playwright install chromium
cd ..
make e2e
```

Для `make e2e` нужен работающий полный стек (`make up`). В CI дополнительно проверяется потеря связи с БД и восстановление.

## Документация

- [Спецификация купонного MVP: клиенты, купоны и история применений](docs/coupon-mvp.md)
- [План реализации MVP и критерии приемки](docs/mvp-backlog.md)
- [Архитектура и границы первого этапа](docs/architecture.md)
- [Локальная разработка и переменные окружения](docs/development.md)
- [Развертывание, обновление, резервные копии и откат](docs/deployment.md)
- [Работа с агентами](docs/agent-workflow.md)

После успешных проверок `main` CI публикует два образа в GHCR с тегом `sha-` и полным SHA коммита. Развертывание на сервер выполняется явно через `make deploy`. В этом этапе нет пользователей, авторизации, купонов, QR-кодов, рефералов или таблиц предметной области.
