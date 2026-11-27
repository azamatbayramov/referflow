# Локальная разработка

## Полный стек в Docker

`make up` собирает API/web, запускает PostgreSQL, выполняет отдельную команду миграции и ждет health checks. `make create-user` создает учетную запись через скрытый ввод пароля. `make smoke` проверяет страницу, health endpoints и неизвестный API-путь. `make down` останавливает стек с сохранением томов.

## Go и Vite на хосте

```bash
make setup
make db
make migrate-host
make create-user-host
make dev-api
```

Во втором терминале — `make dev-web`. Vite слушает `127.0.0.1:5173` и проксирует `/api` на `127.0.0.1:8081`. `make dev-api` читает доверенный локальный `.env` как shell-файл. Не запускайте одновременно контейнерный и хостовый API на одном порту: сначала `docker compose stop api web`.

Если меняете порт API для hot reload, согласованно измените `HTTP_ADDR` в `.env` и proxy target в `frontend/vite.config.ts`. Для тестирования контейнеров на другом `WEB_PORT` передайте тот же URL через `BASE_URL` в `make smoke` и `make e2e`.

## Переменные

| Переменная | Значение для хоста по умолчанию | Назначение |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` в коде, `127.0.0.1:8081` в `.env` | Адрес HTTP-сервера |
| `LOG_LEVEL` | `info` | Уровень структурированных JSON-логов |
| `PGHOST` | `127.0.0.1` | Узел PostgreSQL; Compose задает `db` |
| `PGPORT` | `5432` | Порт PostgreSQL |
| `PGDATABASE` | `referflow` | База |
| `PGUSER` | `referflow` | Пользователь |
| `PGPASSWORD` | Обязательная | Пароль; development-значение есть только в `.env.example` |
| `PGSSLMODE` | `require` | `disable`, `require`, `verify-ca`, `verify-full`; внутри локальной сети Compose задан `disable` |
| `API_PORT` | `8081` | Публикуемый локальный порт контейнера API |
| `WEB_PORT` | `8080` | Публикуемый локальный порт Caddy |
| `COMPOSE_PROJECT_NAME` | `referflow` | Имя локального Compose-проекта |
| `APP_ORIGINS` | Локальные origins 8080/5173 из `.env.example` | Точный список разрешенных origins для POST; при смене WEB_PORT обновить список |
| `SESSION_COOKIE_SECURE` | `true` в Go, `false` в локальном `.env` | HTTP без Secure допустим только на loopback; production всегда true |
| `SESSION_TTL` | `12h` | Абсолютный срок сессии, от 1m до 168h |
| `BASE_URL` | `http://127.0.0.1:8080` | Цель smoke- и browser-проверок |

Не добавляйте пароли или ключи в `VITE_*`: такие значения становятся частью клиентской сборки. Параметры подключения не возвращаются в health-ответах и не печатаются при ошибке конфигурации.

## Проверки

```bash
make check
make up
make smoke
bash scripts/check-database-outage.sh
cd frontend
npx playwright install chromium
cd ..
make e2e
```

`make check` запускает Go formatting/vet/race tests/build и frontend formatting/lint/typecheck/build. Unit-тестам не нужна БД. Browser-проверки проходят через Caddy на desktop/mobile, проверяют загрузку Vue, API-прокси и fallback. Скрипт отказа БД временно останавливает **локальный** PostgreSQL, проверяет 503 у readiness и 200 у liveness, затем восстанавливает стек.

Если изменен только текст документации, достаточно проверить ссылки, команды и `git diff --check`. Если добавлено поведение, добавьте проверку результата и существенных ошибок. Пустые frontend unit-тесты в каркас не включены.

## Данные и миграции

PostgreSQL хранится в именованном томе. Изменение `PGPASSWORD` в `.env` не меняет пароль пользователя в уже созданной БД — потребуется явная смена пароля в PostgreSQL. Не удаляйте том ради изменения конфигурации, если данные нужны.

`backend/migrations/` содержит встроенные SQL-миграции. `make migrate` запускает их из собранного API-образа, `make migrate-host` — через Go. Повторное применение безопасно; изменение уже примененного файла отклоняется. Подробнее — [авторизация](authentication.md).

Интеграционные Go-тесты: `TEST_DATABASE_URL` должен указывать на отдельную тестовую PostgreSQL, затем `make test-integration`. Они создают собственные случайные схемы и удаляют только их. Browser-тесты создают случайные тестовые учетные записи и истекают их сессии: запускать исключительно на disposable-стеке. Для native-стека поддержаны `E2E_MANAGE_BIN`, `E2E_PSQL_BIN` и обычные `PG*` переменные. Полный Compose-путь проверяется в CI.
