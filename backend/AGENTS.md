# Backend

- Go module: `github.com/azamatbayramov/referflow/backend`; executables: `cmd/api` and the operator-only `cmd/manage`.
- `cmd/api` wires lifecycle and dependencies. `internal/config` validates environment. `internal/httpserver` owns HTTP. `internal/postgres` owns the pool.
- Keep handlers small. Add feature packages when concrete business behavior requires them; do not pre-create controller/service/repository layers.
- Pass context to database calls, bound external work, and preserve graceful shutdown.
- Never log a connection string, password, request body, or future patient information. Health errors expose only status.
- Keep `api/openapi.yaml` in sync with HTTP routes and response bodies.
- Add schema migrations only with a schema change. See `migrations/README.md`.
- Checks from this directory: `go fmt ./...`, `go vet ./...`, `go test -race ./...`, `go build ./cmd/api` and `go build -o bin/manage ./cmd/manage`. Use `go build -o bin/api ./cmd/api` to avoid an untracked root binary.
- Unit tests must not require PostgreSQL or credentials. PostgreSQL integration tests use the `integration` build tag and TEST_DATABASE_URL with a disposable database; tests create and remove only their own random schemas. Container connectivity and recovery also belong in the Compose smoke check.
