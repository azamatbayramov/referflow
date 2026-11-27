CREATE UNIQUE INDEX clients_owner_id_idx ON clients (specialist_id,id);
CREATE TABLE coupons (
    id uuid PRIMARY KEY,
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    code text NOT NULL CHECK (code ~ '^[0-9]{6}$'),
    discount_type text NOT NULL CHECK (discount_type IN ('percent','fixed')),
    value_minor bigint NOT NULL CHECK (value_minor > 0 AND value_minor <= 999999999999),
    CHECK (discount_type <> 'percent' OR value_minor <= 10000),
    restrictions text NOT NULL DEFAULT '' CHECK (length(restrictions) <= 2000),
    total_limit integer CHECK (total_limit > 0),
    per_client_limit integer DEFAULT 1 CHECK (per_client_limit > 0),
    first_visit_only boolean NOT NULL DEFAULT false,
    expires_on date,
    expires_at timestamptz,
    CHECK ((expires_on IS NULL) = (expires_at IS NULL)),
    referrer_client_id uuid,
    FOREIGN KEY (specialist_id,referrer_client_id) REFERENCES clients(specialist_id,id),
    used_count integer NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    CHECK (total_limit IS NULL OR used_count <= total_limit),
    revoked_at timestamptz,
    revoke_reason text CHECK (length(revoke_reason) BETWEEN 1 AND 500),
    CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL)),
    creation_key uuid NOT NULL,
    creation_hash bytea NOT NULL CHECK (octet_length(creation_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (specialist_id,code),
    UNIQUE (specialist_id,creation_key)
);
CREATE INDEX coupons_owner_order_idx ON coupons (specialist_id,created_at DESC,id DESC);
CREATE TABLE coupon_events (
    coupon_id uuid NOT NULL REFERENCES coupons(id),
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    action text NOT NULL CHECK (action IN ('created','revoked')),
    reason text,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (coupon_id,action)
);
CREATE TABLE coupon_lookup_limits (
    specialist_id uuid PRIMARY KEY REFERENCES specialists(id),
    attempts integer NOT NULL CHECK (attempts > 0),
    resets_at timestamptz NOT NULL
);
