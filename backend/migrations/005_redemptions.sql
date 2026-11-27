CREATE UNIQUE INDEX coupons_owner_id_idx ON coupons (specialist_id,id);
CREATE TABLE coupon_redemptions (
    id uuid PRIMARY KEY,
    specialist_id uuid NOT NULL REFERENCES specialists(id),
    coupon_id uuid NOT NULL,
    client_id uuid NOT NULL,
    FOREIGN KEY (specialist_id,coupon_id) REFERENCES coupons(specialist_id,id),
    FOREIGN KEY (specialist_id,client_id) REFERENCES clients(specialist_id,id),
    discount_type text NOT NULL CHECK (discount_type IN ('percent','fixed')),
    value_minor bigint NOT NULL CHECK (value_minor > 0),
    single_coupon_confirmed boolean NOT NULL CHECK (single_coupon_confirmed),
    first_visit_confirmed boolean NOT NULL,
    first_visit_required boolean NOT NULL,
    CHECK (NOT first_visit_required OR first_visit_confirmed),
    confirmed_at timestamptz NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
    canceled_at timestamptz,
    canceled_by uuid REFERENCES specialists(id),
    cancel_reason text CHECK (length(cancel_reason) BETWEEN 1 AND 500),
    CHECK ((canceled_at IS NULL AND canceled_by IS NULL AND cancel_reason IS NULL)
        OR (canceled_at IS NOT NULL AND canceled_by IS NOT NULL AND cancel_reason IS NOT NULL)),
    CHECK (canceled_at IS NULL OR canceled_at >= confirmed_at),
    UNIQUE (specialist_id,idempotency_key)
);
CREATE INDEX redemptions_client_history_idx ON coupon_redemptions (specialist_id,client_id,confirmed_at DESC,id DESC);
CREATE INDEX redemptions_coupon_history_idx ON coupon_redemptions (specialist_id,coupon_id,confirmed_at DESC,id DESC);
CREATE INDEX redemptions_personal_active_idx ON coupon_redemptions (specialist_id,coupon_id,client_id) WHERE canceled_at IS NULL;
