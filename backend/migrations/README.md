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

`002_specialist_login.sql` renames `specialists.email` to `login`, including its
constraints. Existing identifiers, account IDs, password hashes, sessions and
login budgets are preserved. An existing email-shaped identifier remains the
account's login. Apply it with the updated API/frontend; the earlier email-based
authentication binary is not compatible with the renamed column. Rolling back
to that binary requires a matching database restore, not just an image rollback.

Local commands: `make migrate` using a built API image and running DB, or
`make migrate-host` using Go and a trusted `.env`. `make up` performs the former
after the database starts; production deployment performs the same separate step.

Run `make test-integration` with `TEST_DATABASE_URL` pointing to a disposable
PostgreSQL instance. Tests create and remove only their own random schemas.
See [authentication](../../docs/authentication.md) and [deployment](../../docs/deployment.md).

`003_clients.sql` adds owner-scoped client cards and immutable operation events.
It is additive; rolling back to the login-based authentication version preserves
these tables. See [client cards](../../docs/clients.md).

`004_coupons.sql` adds coupons, issuance/revocation events and per-specialist
lookup budgets. It also adds an owner/ID unique client index for the composite
referrer FK. Existing migrations remain unchanged. Rolling back to client cards
preserves the added tables. See [coupons](../../docs/coupons.md).
