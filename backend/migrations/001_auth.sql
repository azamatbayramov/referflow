CREATE TABLE specialists (
    id uuid PRIMARY KEY,
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);
CREATE INDEX sessions_specialist_idx ON sessions (specialist_id);

-- Shared across API replicas; keys contain hashes, never email addresses.
CREATE TABLE login_limits (
    key text PRIMARY KEY,
    attempts integer NOT NULL CHECK (attempts > 0),
    resets_at timestamptz NOT NULL
);
CREATE INDEX login_limits_expiry_idx ON login_limits (resets_at);
