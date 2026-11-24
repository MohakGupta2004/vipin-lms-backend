-- One row per completed diagnostic test. The questions live in the frontend,
-- so the backend only stores the score, the per-element breakdown and the answers.
CREATE TABLE diagnostic_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    correct int NOT NULL,
    total int NOT NULL,
    elements jsonb NOT NULL DEFAULT '[]'::jsonb,
    answers jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX diagnostic_results_user_idx ON diagnostic_results (user_id);
CREATE INDEX diagnostic_results_created_idx ON diagnostic_results (created_at);
