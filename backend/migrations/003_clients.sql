CREATE TABLE clients (
    id uuid PRIMARY KEY,
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    name_search text NOT NULL,
    phone text NOT NULL DEFAULT '',
    phone_digits text NOT NULL DEFAULT '',
    archived boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    creation_key uuid NOT NULL,
    creation_hash bytea NOT NULL CHECK (octet_length(creation_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (specialist_id, creation_key)
);
CREATE INDEX clients_owner_order_idx ON clients (specialist_id, created_at DESC, id DESC);
CREATE INDEX clients_owner_phone_idx ON clients (specialist_id, phone_digits);

CREATE TABLE client_events (
    client_id uuid NOT NULL REFERENCES clients(id),
    version bigint NOT NULL,
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    action text NOT NULL CHECK (action IN ('created','updated','archived','restored')),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (client_id, version)
);
