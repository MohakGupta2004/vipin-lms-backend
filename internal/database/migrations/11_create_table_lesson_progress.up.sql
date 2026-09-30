-- Upserted on every progress ping.
CREATE TABLE lesson_progress (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lesson_id UUID NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,

    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'completed')),
    last_position_sec INT NOT NULL DEFAULT 0,
    completed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (user_id, lesson_id)
);

-- user_id is covered by the unique (user_id, lesson_id) index.
CREATE INDEX lesson_progress_lesson_id_idx ON lesson_progress (lesson_id);

CREATE TRIGGER lesson_progress_set_updated_at
    BEFORE UPDATE ON lesson_progress
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
