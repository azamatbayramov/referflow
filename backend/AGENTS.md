# Backend

- Go module: `github.com/azamatbayramov/referflow/backend`; executable: `cmd/api`.
- `cmd/api` wires lifecycle and dependencies. `internal/config` validates environment. `internal/httpserver` owns HTTP. `internal/postgres` owns the pool.
- Keep handlers small. Add feature packages when concrete business behavior requires them; do not pre-create controller/service/repository layers.
- Pass context to database calls, bound external work, and preserve graceful shutdown.
- Never log a connection string, password, request body, or future patient information. Health errors expose only status.
- Keep `api/openapi.yaml` in sync with HTTP routes and response bodies.
- Add schema migrations only with a schema change. See `migrations/README.md`.
- Checks from this directory: `go fmt ./...`, `go vet ./...`, `go test -race ./...`, `go build ./cmd/api`. Use `go build -o bin/api ./cmd/api` to avoid an untracked root binary.
- Unit tests must not require PostgreSQL or credentials. Real connectivity and recovery belong in the Compose smoke check.
