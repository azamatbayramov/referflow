ALTER TABLE coupons ADD COLUMN template_id uuid;
ALTER TABLE coupons ADD CONSTRAINT coupons_template_owner_fk
    FOREIGN KEY (specialist_id, template_id) REFERENCES coupon_templates(specialist_id, id);
CREATE INDEX coupons_template_idx ON coupons(specialist_id, template_id, created_at DESC, id DESC);
