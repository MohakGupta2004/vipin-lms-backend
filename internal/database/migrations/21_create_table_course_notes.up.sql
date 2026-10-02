-- PDF notes an instructor uploads for a course they teach.
-- The file itself lives in the GCS bucket; object_key points to it.
-- Visible to the course instructor and to students with a valid enrollment.
-- (Not the same as "notes", which holds students' own text notes.)
CREATE TABLE course_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    uploaded_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    title TEXT NOT NULL,
    description TEXT,
    file_name TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Listed newest first per course.
CREATE INDEX course_notes_course_id_created_at_idx ON course_notes (course_id, created_at DESC);

CREATE TRIGGER course_notes_set_updated_at
    BEFORE UPDATE ON course_notes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
