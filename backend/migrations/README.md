# Database migrations

No schema is created by the scaffold. The pool and readiness probe connect to an empty PostgreSQL database.

Add versioned SQL migrations and a migration command with the first persistence feature. Run migrations as an explicit deployment step, not from every API replica. Document upgrade/rollback behavior and test against a clean database and the previous schema. Never edit an already applied migration.
