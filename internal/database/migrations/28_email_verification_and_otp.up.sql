ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- One-time codes for email verification and password reset.
-- Only keyed hashes are stored, never the code or the reset token itself.
CREATE TABLE otp_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL CHECK (purpose IN ('email_verification', 'password_reset')),
    code_hash TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,

    -- password_reset only: set once the code is verified; the reset token authorizes the new password.
    verified_at TIMESTAMPTZ,
    reset_token_hash TEXT,
    reset_expires_at TIMESTAMPTZ,

    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX otp_codes_user_purpose_idx ON otp_codes (user_id, purpose, created_at DESC);
CREATE UNIQUE INDEX otp_codes_reset_token_idx ON otp_codes (reset_token_hash) WHERE reset_token_hash IS NOT NULL;
