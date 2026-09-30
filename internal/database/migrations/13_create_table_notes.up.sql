-- private = only the author sees it (students)
-- course  = all enrolled students see it (only instructors/admins may set this, enforced in Go)
CREATE TABLE notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id UUID REFERENCES lessons(id) ON DELETE CASCADE,

    title TEXT,
    content TEXT NOT NULL,
    video_timestamp_sec INT,
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'course')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX notes_author_id_idx ON notes (author_id);
CREATE INDEX notes_course_id_idx ON notes (course_id);
CREATE INDEX notes_lesson_id_idx ON notes (lesson_id);

CREATE TRIGGER notes_set_updated_at
    BEFORE UPDATE ON notes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
