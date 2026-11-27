# Deployment

- `compose.yaml` in this directory is a standalone production file. Do not merge it with the root development Compose file, which publishes database/API ports.
- API/web production images must use the same full commit tag; never use `latest` for a release.
- Validate with `docker compose --env-file .env.production -f deploy/compose.yaml config --quiet`. Do not print resolved configuration: it contains credentials.
- Keep database/API ports private, PostgreSQL/Caddy data persistent, image health checks intact, and shutdown grace longer than the API's shutdown timeout.
- Deployment is an explicit operator action. Publishing images after successful main CI does not authorize deploying to a server.
- `docker compose down --volumes` destroys data. Never use it on a persistent deployment.
- Document new variables, rollout, backups, and rollback in `docs/deployment.md`. Update root development Compose if a shared dependency changes.
