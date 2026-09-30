CREATE TABLE course_sections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,

    title TEXT NOT NULL,
    description TEXT,
    position INT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX course_sections_course_id_idx ON course_sections (course_id);

CREATE TRIGGER course_sections_set_updated_at
    BEFORE UPDATE ON course_sections
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
