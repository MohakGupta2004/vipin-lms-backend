-- Decides who is premium. Admin inserts rows today, Stripe webhook later.
-- Is premium = status = 'active' AND starts_at <= now() AND (ends_at IS NULL OR ends_at > now())
CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    plan TEXT NOT NULL DEFAULT 'premium' CHECK (plan IN ('premium')),
    status TEXT NOT NULL CHECK (status IN ('active', 'cancelled', 'expired')),
    starts_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at TIMESTAMPTZ,

    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'stripe')),
    external_id TEXT,
    granted_by UUID REFERENCES users(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_user_id_status_idx ON subscriptions (user_id, status);
CREATE INDEX subscriptions_granted_by_idx ON subscriptions (granted_by);

CREATE TRIGGER subscriptions_set_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
