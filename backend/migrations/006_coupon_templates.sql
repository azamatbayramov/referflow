CREATE TABLE coupon_templates (
    id uuid PRIMARY KEY,
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    discount_type text NOT NULL CHECK (discount_type IN ('percent','fixed')),
    value_minor bigint NOT NULL CHECK (value_minor > 0 AND value_minor <= 999999999999),
    CHECK (discount_type <> 'percent' OR value_minor <= 10000),
    restrictions text NOT NULL DEFAULT '' CHECK (length(restrictions) <= 2000),
    total_limit integer CHECK (total_limit > 0),
    per_client_limit integer DEFAULT 1 CHECK (per_client_limit > 0),
    first_visit_only boolean NOT NULL DEFAULT false,
    expires_on date,
    archived boolean NOT NULL DEFAULT false,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (specialist_id,id)
);
CREATE INDEX coupon_templates_owner_order_idx ON coupon_templates (specialist_id,created_at DESC,id DESC);
-- Retain successful mutation keys even after later edits or archival.
CREATE TABLE coupon_template_operations (
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    idempotency_key uuid NOT NULL,
    template_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (specialist_id,idempotency_key),
    FOREIGN KEY (specialist_id,template_id) REFERENCES coupon_templates(specialist_id,id)
);
