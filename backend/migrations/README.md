# Database migrations

Versioned SQL files are embedded in the `manage` executable. Run `manage migrate`
as an explicit operator/deployment step before the new API starts. API replicas
never create or migrate the schema. Readiness verifies required migration checksums.

The runner uses one transaction and a PostgreSQL advisory lock. Applied versions
are recorded with SHA-256 checksums in `schema_migrations`; repeat runs are safe,
and an edited applied file is rejected. Add the next numbered SQL file rather
than changing an applied one. There are no automatic down migrations.

`001_auth.sql` adds specialist accounts, hashed sessions and shared login budgets.
It is additive and compatible with rolling the application back to the earlier
infrastructure-only scaffold. Preserve these tables on application rollback.
Back up before deployment and test restoration before incompatible schema changes.

Local commands: `make migrate` using a built API image and running DB, or
`make migrate-host` using Go and a trusted `.env`. `make up` performs the former
after the database starts; production deployment performs the same separate step.

Run `make test-integration` with `TEST_DATABASE_URL` pointing to a disposable
PostgreSQL instance. Tests create and remove only their own random schemas.
See [authentication](../../docs/authentication.md) and [deployment](../../docs/deployment.md).
