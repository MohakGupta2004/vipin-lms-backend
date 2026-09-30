CREATE TABLE quizzes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id UUID REFERENCES lessons(id) ON DELETE SET NULL,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    title TEXT NOT NULL,
    description TEXT,
    time_limit_sec INT, -- NULL = untimed
    pass_percent INT NOT NULL DEFAULT 70 CHECK (pass_percent BETWEEN 0 AND 100),
    is_free BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX quizzes_course_id_idx ON quizzes (course_id);
CREATE INDEX quizzes_lesson_id_idx ON quizzes (lesson_id);
CREATE INDEX quizzes_created_by_idx ON quizzes (created_by);

CREATE TRIGGER quizzes_set_updated_at
    BEFORE UPDATE ON quizzes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
