DROP TABLE IF EXISTS entitlements;

DROP TRIGGER IF EXISTS trigger_subscriptions_updated_at ON subscriptions;
DROP FUNCTION IF EXISTS update_subscriptions_updated_at();
DROP TABLE IF EXISTS subscriptions;

DROP TABLE IF EXISTS plans;

DROP TYPE IF EXISTS subscription_status;
DROP TYPE IF EXISTS plan_feature;
DROP TYPE IF EXISTS billing_cycle;
