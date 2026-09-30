CREATE TABLE lessons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    section_id UUID NOT NULL REFERENCES course_sections(id) ON DELETE CASCADE,
    -- Duplicated from section on purpose so access checks are one join.
    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,

    title TEXT NOT NULL,
    lesson_type TEXT NOT NULL CHECK (lesson_type IN ('video', 'article')),
    content TEXT,
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,

    is_free BOOLEAN NOT NULL DEFAULT false,
    is_published BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX lessons_section_id_idx ON lessons (section_id);
CREATE INDEX lessons_course_id_idx ON lessons (course_id);
CREATE INDEX lessons_video_id_idx ON lessons (video_id);

CREATE TRIGGER lessons_set_updated_at
    BEFORE UPDATE ON lessons
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
