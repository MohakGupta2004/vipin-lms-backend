CREATE TABLE courses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    exam_id UUID NOT NULL REFERENCES exams(id) ON DELETE RESTRICT,
    instructor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    title TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    short_description TEXT,
    description TEXT,
    thumbnail_key TEXT, -- GCS object key

    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX courses_exam_id_idx ON courses (exam_id);
CREATE INDEX courses_instructor_id_idx ON courses (instructor_id);

CREATE TRIGGER courses_set_updated_at
    BEFORE UPDATE ON courses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
