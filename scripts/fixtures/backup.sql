-- Artificial, isolated backup-check database only. No real accounts or contacts.
DO $fixture$
DECLARE
    fixture_owner uuid := gen_random_uuid();
    fixture_client uuid := gen_random_uuid();
    fixture_coupon uuid := gen_random_uuid();
BEGIN
    INSERT INTO specialists(id,login,name,password_hash)
      VALUES(fixture_owner,'backup-fixture','Тестовый специалист','fixture-cannot-authenticate');
    INSERT INTO clients(id,specialist_id,name,name_search,creation_key,creation_hash)
      VALUES(fixture_client,fixture_owner,'Тестовый клиент','тестовый клиент',gen_random_uuid(),decode(repeat('ab',32),'hex'));
    INSERT INTO client_events(client_id,version,specialist_id,action) VALUES(fixture_client,1,fixture_owner,'created');
    INSERT INTO coupons(id,specialist_id,code,discount_type,value_minor,restrictions,total_limit,per_client_limit,first_visit_only,referrer_client_id,used_count,creation_key,creation_hash)
      VALUES(fixture_coupon,fixture_owner,'012345','percent',1250,'Тестовые условия',2,2,true,fixture_client,1,gen_random_uuid(),decode(repeat('cd',32),'hex'));
    INSERT INTO coupon_events(coupon_id,specialist_id,action) VALUES(fixture_coupon,fixture_owner,'created');
    INSERT INTO coupon_redemptions(id,specialist_id,coupon_id,client_id,discount_type,value_minor,single_coupon_confirmed,first_visit_confirmed,first_visit_required,confirmed_at,idempotency_key,request_hash)
      VALUES(gen_random_uuid(),fixture_owner,fixture_coupon,fixture_client,'percent',1250,true,true,true,clock_timestamp()-interval '1 minute',gen_random_uuid(),decode(repeat('ef',32),'hex'));
    INSERT INTO coupon_redemptions(id,specialist_id,coupon_id,client_id,discount_type,value_minor,single_coupon_confirmed,first_visit_confirmed,first_visit_required,confirmed_at,idempotency_key,request_hash,canceled_at,canceled_by,cancel_reason)
      VALUES(gen_random_uuid(),fixture_owner,fixture_coupon,fixture_client,'percent',1250,true,true,true,clock_timestamp()-interval '1 minute',gen_random_uuid(),decode(repeat('12',32),'hex'),clock_timestamp(),fixture_owner,'Тестовая отмена');
    INSERT INTO sessions(token_hash,specialist_id,expires_at) VALUES(decode(repeat('34',32),'hex'),fixture_owner,now()+interval '1 hour');
    INSERT INTO login_limits(key,attempts,resets_at) VALUES('artificial-backup-key',1,now()+interval '1 minute');
    INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES(fixture_owner,1,now()+interval '1 minute');
END
$fixture$;
